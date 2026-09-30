package notificacao

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/makiuchi-d/gozxing"
	zxingqr "github.com/makiuchi-d/gozxing/qrcode"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/outbox"
)

func TestEfeito(t *testing.T) {
	agora := time.Date(2098, 1, 1, 12, 0, 3, 0, time.UTC)
	var latencias []time.Duration
	var entregues []uuid.UUID
	falhar := false
	c := NovoConsumidor(Config{
		Agora:    func() time.Time { return agora },
		Latencia: func(_ context.Context, d time.Duration) { latencias = append(latencias, d) },
		Entregar: func(_ context.Context, id uuid.UUID) error {
			if falhar {
				return errors.New("smtp fora")
			}
			entregues = append(entregues, id)
			return nil
		},
	}, zap.NewNop())
	id := uuid.New()
	msg := outbox.Mensagem{Payload: []byte(`{"pedido_id":"` + id.String() + `"}`), OccurredAt: agora.Add(-3 * time.Second)}

	if err := c.Efeito(context.Background(), nil, msg); err != nil || len(entregues) != 1 || entregues[0] != id ||
		len(latencias) != 1 || latencias[0] != 3*time.Second {
		t.Fatalf("efeito: %v entregues=%v latencias=%v", err, entregues, latencias)
	}

	// Falha na entrega é transitória (redelivery → DLQ), nunca permanente.
	falhar = true
	if err := c.Efeito(context.Background(), nil, msg); err == nil || errors.Is(err, outbox.ErrPermanente) {
		t.Fatalf("falha de entrega: %v", err)
	}

	// Payload inválido vai direto para a DLQ.
	for nome, corpo := range map[string]string{"ilegível": "{", "sem id": `{}`, "id inválido": `{"pedido_id":"x"}`} {
		if err := c.Efeito(context.Background(), nil, outbox.Mensagem{Payload: []byte(corpo)}); !errors.Is(err, outbox.ErrPermanente) {
			t.Errorf("%s: %v", nome, err)
		}
	}

	stub := NovoConsumidor(Config{}, zap.NewNop())
	if err := stub.Efeito(context.Background(), nil, msg); err != nil {
		t.Fatalf("stub: %v", err)
	}
}

// --- E-mail de confirmação (PRD 0028) ---

var atualizarGolden = flag.Bool("update", false, "regrava os arquivos golden")

type fonteFixa struct {
	d   DadosEmail
	err error
}

func (f fonteFixa) Carregar(context.Context, uuid.UUID) (DadosEmail, error) { return f.d, f.err }

func dadosTeste() DadosEmail {
	return DadosEmail{
		PedidoID: uuid.MustParse("11111111-2222-3333-4444-555555555555"),
		Para:     "ana@exemplo.com", Codigo: "ABCDEFGHIJKLMNOP",
		Filme:  "Duna: Parte Dois",
		Inicio: time.Date(2099, 1, 1, 23, 30, 0, 0, time.UTC), // 20:30 em Brasília
		Sala:   "Sala 1", TotalCentavos: 123456,
		Ingressos: []IngressoEmail{
			{ID: uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001"), Assento: "A1", Token: strings.Repeat("t", 43)},
			{ID: uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000002"), Assento: "A2", Token: strings.Repeat("u", 43)},
		},
	}
}

func entregadorTeste(t *testing.T, f FonteDoEmail, s EmailSender) *Entregador {
	t.Helper()
	e, err := NovoEntregador(f, s, "https://morfeu.exemplo/")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// TestMontar_Golden cobre CA03: HTML e texto estáveis (datas em Brasília).
func TestMontar_Golden(t *testing.T) {
	m, err := entregadorTeste(t, fonteFixa{}, NovoFake()).Montar(dadosTeste())
	if err != nil {
		t.Fatal(err)
	}
	obtido := "== HTML ==\n" + m.HTML + "== TEXTO ==\n" + m.Texto
	caminho := filepath.Join("testdata", "confirmacao.golden")
	if *atualizarGolden {
		if err := os.WriteFile(caminho, []byte(obtido), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	esperado, err := os.ReadFile(caminho) //nolint:gosec // arquivo fixo do teste
	if err != nil {
		t.Fatal(err)
	}
	if obtido != string(esperado) {
		t.Fatalf("golden diverge (rode com -update e revise o diff):\n%s", obtido)
	}
	if m.Assunto != "Seus ingressos — Morfeu" || m.Tipo != TipoConfirmacao ||
		m.ChaveIdempotencia != "confirmacao-11111111-2222-3333-4444-555555555555" || m.Para != "ana@exemplo.com" {
		t.Fatalf("cabeçalhos: %+v", m)
	}
}

// TestMontar_QRDecodificavel cobre CA02: um QR por ingresso, inline por CID,
// que decodifica para o link /i/{id}.{token}.
func TestMontar_QRDecodificavel(t *testing.T) {
	d := dadosTeste()
	m, err := entregadorTeste(t, fonteFixa{}, NovoFake()).Montar(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Anexos) != 2 {
		t.Fatalf("anexos: %d", len(m.Anexos))
	}
	for i, a := range m.Anexos {
		ing := d.Ingressos[i]
		if a.ContentID != "ingresso-"+ing.Assento || a.Tipo != "image/png" || !strings.Contains(m.HTML, `src="cid:`+a.ContentID+`"`) {
			t.Fatalf("anexo %d: %+v", i, a)
		}
		img, err := png.Decode(bytes.NewReader(a.Conteudo))
		if err != nil {
			t.Fatal(err)
		}
		bmp, err := gozxing.NewBinaryBitmapFromImage(img)
		if err != nil {
			t.Fatal(err)
		}
		r, err := zxingqr.NewQRCodeReader().Decode(bmp, nil)
		if err != nil {
			t.Fatalf("QR ilegível: %v", err)
		}
		if quer := "https://morfeu.exemplo/i/" + ing.ID.String() + "." + ing.Token; r.GetText() != quer {
			t.Fatalf("QR = %q, quer %q", r.GetText(), quer)
		}
	}
}

// TestMontar_Seguranca cobre CA04: escape de dado de terceiro (título do
// TMDB), nenhuma imagem externa nem pixel, links só da base configurada.
func TestMontar_Seguranca(t *testing.T) {
	d := dadosTeste()
	d.Filme = `<script>alert(1)</script><img src="https://rastreio.exemplo/p.gif">`
	m, err := entregadorTeste(t, fonteFixa{}, NovoFake()).Montar(d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m.HTML, "<script>") || strings.Contains(m.HTML, `<img src="https://`) {
		t.Fatalf("HTML sem escape:\n%s", m.HTML)
	}
	for _, src := range regexp.MustCompile(`src="([^"]*)"`).FindAllStringSubmatch(m.HTML, -1) {
		if !strings.HasPrefix(src[1], "cid:") {
			t.Fatalf("imagem fora de cid: %s", src[1])
		}
	}
	for _, href := range regexp.MustCompile(`href="([^"]*)"`).FindAllStringSubmatch(m.HTML, -1) {
		if !strings.HasPrefix(href[1], "https://morfeu.exemplo/i/") {
			t.Fatalf("link fora da base: %s", href[1])
		}
	}
	if strings.Contains(m.Assunto, d.Filme) {
		t.Fatal("assunto com dado livre")
	}
	if _, err := NovoEntregador(fonteFixa{}, NovoFake(), "javascript:alert(1)"); err == nil {
		t.Fatal("base não http(s) deveria ser recusada")
	}
}

// TestEntregar cobre CA05: fonte → envio; não notificável e inexistente
// nunca enviam; falha do provedor sobe (transitório).
func TestEntregar(t *testing.T) {
	ctx := context.Background()
	fake := NovoFake()
	if err := entregadorTeste(t, fonteFixa{d: dadosTeste()}, fake).Entregar(ctx, uuid.New()); err != nil || len(fake.Enviadas()) != 1 {
		t.Fatalf("envio: %v %d", err, len(fake.Enviadas()))
	}
	for _, e := range []error{ErrNaoNotificavel, ErrPedidoInexistente} {
		f := NovoFake()
		if err := entregadorTeste(t, fonteFixa{err: e}, f).Entregar(ctx, uuid.New()); !errors.Is(err, e) || len(f.Enviadas()) != 0 {
			t.Fatalf("%v: %v enviadas=%d", e, err, len(f.Enviadas()))
		}
	}
	falho := NovoFake()
	falho.FalharPrimeiras(1)
	if err := entregadorTeste(t, fonteFixa{d: dadosTeste()}, falho).Entregar(ctx, uuid.New()); err == nil {
		t.Fatal("falha do provedor deveria subir")
	}
}

// TestEfeito_ErrosDaEntrega cobre CA06: não notificável = ack sem latência;
// inexistente = permanente (DLQ).
func TestEfeito_ErrosDaEntrega(t *testing.T) {
	var latencias int
	c := func(err error) *ConsumidorPedidos {
		return NovoConsumidor(Config{
			Latencia: func(context.Context, time.Duration) { latencias++ },
			Entregar: func(context.Context, uuid.UUID) error { return err },
		}, zap.NewNop())
	}
	msg := outbox.Mensagem{Payload: []byte(`{"pedido_id":"` + uuid.NewString() + `"}`), OccurredAt: time.Now()}
	if err := c(ErrNaoNotificavel).Efeito(context.Background(), nil, msg); err != nil || latencias != 0 {
		t.Fatalf("não notificável: %v latências=%d", err, latencias)
	}
	if err := c(ErrPedidoInexistente).Efeito(context.Background(), nil, msg); !errors.Is(err, outbox.ErrPermanente) {
		t.Fatalf("inexistente: %v", err)
	}
}

func TestFormatarBRL(t *testing.T) {
	for c, quer := range map[int64]string{0: "R$ 0,00", 3000: "R$ 30,00", 123456: "R$ 1.234,56", 100000000: "R$ 1.000.000,00"} {
		if got := formatarBRL(c); got != quer {
			t.Errorf("%d: %s", c, got)
		}
	}
}

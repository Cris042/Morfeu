package notificacao

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"strings"
	"sync"
	texttemplate "text/template"
	"time"
	_ "time/tzdata" // America/Sao_Paulo na imagem scratch (sem zoneinfo do SO)

	"github.com/google/uuid"
	qrcode "github.com/skip2/go-qrcode"
)

// Erros da montagem/entrega do e-mail.
var (
	// ErrNaoNotificavel: pedido não pago ou sem ingresso ativo — nada a enviar
	// (ack sem envio; nunca um QR de ingresso inválido).
	ErrNaoNotificavel = errors.New("notificacao: pedido não notificável")
	// ErrPedidoInexistente é permanente: nenhum replay faz o pedido existir.
	ErrPedidoInexistente = errors.New("notificacao: pedido inexistente")
)

// Tipos de e-mail (label `tipo` das métricas — lista fechada).
const (
	TipoConfirmacao = "confirmacao"
	TipoEstorno     = "estorno" // PRD 0030
)

// fusoCinema: o cinema e o cliente estão no horário de Brasília.
const fusoCinema = "America/Sao_Paulo"

// tamanhoQR em pixels (PNG): legível no celular e leve no e-mail.
const tamanhoQR = 256

// IngressoEmail é um ingresso ativo com o token já calculado pelo pedido.
type IngressoEmail struct {
	ID      uuid.UUID
	Assento string
	Token   string
}

// DadosEmail é o que o e-mail de confirmação mostra — composto no main a
// partir de pedido, sessão e catálogo (a notificação não lê tabela alheia).
type DadosEmail struct {
	PedidoID      uuid.UUID
	Para          string
	Codigo        string
	Filme         string
	Inicio        time.Time
	Sala          string
	TotalCentavos int64
	Ingressos     []IngressoEmail
}

// FonteDoEmail carrega os dados do pedido (porta ligada no main — ADR 0003).
// Devolve ErrNaoNotificavel ou ErrPedidoInexistente conforme o caso.
type FonteDoEmail interface {
	Carregar(ctx context.Context, pedidoID uuid.UUID) (DadosEmail, error)
}

// Anexo é um arquivo do e-mail; ContentID não vazio = imagem inline (cid:).
type Anexo struct {
	Nome      string
	ContentID string
	Tipo      string
	Conteudo  []byte
}

// Mensagem é o e-mail pronto para o provedor.
type Mensagem struct {
	Tipo              string
	Para              string
	Assunto           string
	HTML              string
	Texto             string
	Anexos            []Anexo
	ChaveIdempotencia string // {tipo}-{pedido_id}: reenvio não duplica (Resend, 24 h)
}

// EmailSender é a porta do provedor (Strategy — ADR 0005): Resend × fake.
type EmailSender interface {
	Enviar(ctx context.Context, m Mensagem) error
}

// Entregador compõe fonte → montagem → envio; é o Config.Entregar do consumidor.
type Entregador struct {
	fonte   FonteDoEmail
	sender  EmailSender
	baseURL string
	fuso    *time.Location
}

// NovoEntregador valida a URL pública base (só dela saem os links — nunca do
// header Host).
func NovoEntregador(fonte FonteDoEmail, sender EmailSender, baseURL string) (*Entregador, error) {
	if fonte == nil || sender == nil {
		return nil, errors.New("notificacao: fonte ou sender ausente")
	}
	if !strings.HasPrefix(baseURL, "https://") && !strings.HasPrefix(baseURL, "http://") {
		return nil, errors.New("notificacao: BASE_URL_PUBLICA deve ser uma URL http(s)")
	}
	fuso, err := time.LoadLocation(fusoCinema)
	if err != nil {
		return nil, fmt.Errorf("notificacao: fuso: %w", err)
	}
	return &Entregador{fonte: fonte, sender: sender, baseURL: strings.TrimRight(baseURL, "/"), fuso: fuso}, nil
}

// Entregar carrega o pedido, monta o e-mail de confirmação e envia.
func (e *Entregador) Entregar(ctx context.Context, pedidoID uuid.UUID) error {
	d, err := e.fonte.Carregar(ctx, pedidoID)
	if err != nil {
		return err
	}
	m, err := e.Montar(d)
	if err != nil {
		return err
	}
	return e.sender.Enviar(ctx, m)
}

// LinkIngresso é o endereço público do ingresso (a página é do E8):
// /i/{ingresso_id}.{token} — o servidor recomputa o HMAC e compara.
func (e *Entregador) LinkIngresso(i IngressoEmail) string {
	return fmt.Sprintf("%s/i/%s.%s", e.baseURL, i.ID, i.Token)
}

type ingressoView struct {
	Assento string
	CID     string
	Link    string
}

type confirmacaoView struct {
	Codigo  string
	Filme   string
	Quando  string
	Sala    string
	Total   string
	Tickets []ingressoView
}

// QR gera o PNG do ingresso (256 px). O mesmo gerador serve a página do
// ingresso (PRD 0034), injetado no pedido pelo main.
func QR(conteudo string) ([]byte, error) {
	return qrcode.Encode(conteudo, qrcode.Medium, tamanhoQR)
}

// Montar produz o e-mail de confirmação: HTML (html/template, escape
// automático) + texto puro + um QR PNG inline por ingresso.
func (e *Entregador) Montar(d DadosEmail) (Mensagem, error) {
	v := confirmacaoView{
		Codigo: d.Codigo, Filme: d.Filme, Sala: d.Sala,
		Quando: d.Inicio.In(e.fuso).Format("02/01/2006 às 15:04"),
		Total:  formatarBRL(d.TotalCentavos),
	}
	m := Mensagem{
		Tipo: TipoConfirmacao, Para: d.Para, Assunto: "Seus ingressos — Morfeu",
		ChaveIdempotencia: TipoConfirmacao + "-" + d.PedidoID.String(),
	}
	for _, i := range d.Ingressos {
		link := e.LinkIngresso(i)
		png, err := QR(link)
		if err != nil {
			return Mensagem{}, fmt.Errorf("notificacao: gerar QR: %w", err)
		}
		cid := "ingresso-" + i.Assento
		m.Anexos = append(m.Anexos, Anexo{Nome: cid + ".png", ContentID: cid, Tipo: "image/png", Conteudo: png})
		// O prefixo "cid:" fica literal no template: o html/template bloqueia
		// esquema não-http vindo de dado (viraria #ZgotmplZ).
		v.Tickets = append(v.Tickets, ingressoView{Assento: i.Assento, CID: cid, Link: link})
	}
	var html, texto bytes.Buffer
	if err := templatesHTML().Execute(&html, v); err != nil {
		return Mensagem{}, fmt.Errorf("notificacao: template HTML: %w", err)
	}
	if err := templatesTexto().Execute(&texto, v); err != nil {
		return Mensagem{}, fmt.Errorf("notificacao: template texto: %w", err)
	}
	m.HTML, m.Texto = html.String(), texto.String()
	return m, nil
}

func formatarBRL(centavos int64) string {
	reais, cent := centavos/100, centavos%100
	s := fmt.Sprint(reais)
	var partes []string
	for len(s) > 3 {
		partes = append([]string{s[len(s)-3:]}, partes...)
		s = s[:len(s)-3]
	}
	partes = append([]string{s}, partes...)
	return fmt.Sprintf("R$ %s,%02d", strings.Join(partes, "."), cent)
}

// Templates fixos (sem arquivo externo): nenhum template.HTML, nenhuma imagem
// externa, nenhum pixel de rastreamento; imagens só por cid:.
const htmlConfirmacao = `<!doctype html>
<html lang="pt-BR"><head><meta charset="utf-8"><title>Seus ingressos</title></head>
<body style="font-family:sans-serif;color:#14101F">
<h1>Seus ingressos estão aqui</h1>
<p><strong>{{.Filme}}</strong><br>{{.Quando}} · {{.Sala}}</p>
<p>Pedido <strong>{{.Codigo}}</strong> · Total {{.Total}}</p>
{{range .Tickets}}<div style="margin:16px 0">
<p>Assento <strong>{{.Assento}}</strong></p>
<img src="cid:{{.CID}}" alt="QR do ingresso {{.Assento}}" width="200" height="200">
<p><a href="{{.Link}}">Abrir ingresso {{.Assento}}</a></p>
</div>{{end}}
<p>Apresente o QR na entrada. Guarde o código do pedido para consultas.</p>
</body></html>
`

const textoConfirmacao = `Seus ingressos estão aqui

{{.Filme}}
{{.Quando}} · {{.Sala}}
Pedido {{.Codigo}} · Total {{.Total}}
{{range .Tickets}}
Assento {{.Assento}}: {{.Link}}{{end}}

Apresente o QR na entrada. Guarde o código do pedido para consultas.
`

var (
	tplHTML  = sync.OnceValue(func() *htmltemplate.Template { return htmltemplate.Must(htmltemplate.New("h").Parse(htmlConfirmacao)) })
	tplTexto = sync.OnceValue(func() *texttemplate.Template { return texttemplate.Must(texttemplate.New("t").Parse(textoConfirmacao)) })
)

func templatesHTML() *htmltemplate.Template  { return tplHTML() }
func templatesTexto() *texttemplate.Template { return tplTexto() }

// DadosEstorno é o que o aviso de estorno mostra: sem ingresso, sem token.
type DadosEstorno struct {
	PedidoID      uuid.UUID
	Para          string
	Codigo        string
	TotalCentavos int64
}

// FonteDoEstorno carrega o pedido estornado (porta ligada no main).
// Devolve ErrNaoNotificavel (não estornado) ou ErrPedidoInexistente.
type FonteDoEstorno interface {
	CarregarEstorno(ctx context.Context, pedidoID uuid.UUID) (DadosEstorno, error)
}

// AvisoDeEstorno entrega o e-mail de estorno (PRD 0030) — o Config.Entregar
// do consumidor de pedido.estornado.
type AvisoDeEstorno struct {
	fonte  FonteDoEstorno
	sender EmailSender
}

// NovoAvisoDeEstorno exige fonte e sender.
func NovoAvisoDeEstorno(fonte FonteDoEstorno, sender EmailSender) (*AvisoDeEstorno, error) {
	if fonte == nil || sender == nil {
		return nil, errors.New("notificacao: fonte ou sender ausente")
	}
	return &AvisoDeEstorno{fonte: fonte, sender: sender}, nil
}

// Entregar carrega o pedido estornado, monta o aviso e envia.
func (a *AvisoDeEstorno) Entregar(ctx context.Context, pedidoID uuid.UUID) error {
	d, err := a.fonte.CarregarEstorno(ctx, pedidoID)
	if err != nil {
		return err
	}
	m, err := MontarEstorno(d)
	if err != nil {
		return err
	}
	return a.sender.Enviar(ctx, m)
}

type estornoView struct {
	Codigo string
	Total  string
}

// MontarEstorno produz o aviso: texto curto, sem QR, sem link de ingresso
// (refinamento E7, security) — o ingresso foi cancelado.
func MontarEstorno(d DadosEstorno) (Mensagem, error) {
	v := estornoView{Codigo: d.Codigo, Total: formatarBRL(d.TotalCentavos)}
	var html, texto bytes.Buffer
	if err := tplEstornoHTML().Execute(&html, v); err != nil {
		return Mensagem{}, fmt.Errorf("notificacao: template HTML do estorno: %w", err)
	}
	if err := tplEstornoTexto().Execute(&texto, v); err != nil {
		return Mensagem{}, fmt.Errorf("notificacao: template texto do estorno: %w", err)
	}
	return Mensagem{
		Tipo: TipoEstorno, Para: d.Para, Assunto: "Seu estorno foi concluído — Morfeu",
		HTML: html.String(), Texto: texto.String(),
		ChaveIdempotencia: TipoEstorno + "-" + d.PedidoID.String(),
	}, nil
}

const htmlEstorno = `<!doctype html>
<html lang="pt-BR"><head><meta charset="utf-8"><title>Estorno concluído</title></head>
<body style="font-family:sans-serif;color:#14101F">
<h1>Seu estorno foi concluído</h1>
<p>Não foi possível confirmar os assentos do pedido <strong>{{.Codigo}}</strong>, e o valor de {{.Total}} foi devolvido ao seu cartão.</p>
<p>O prazo para aparecer na fatura depende do banco emissor. Nenhum ingresso deste pedido é válido.</p>
</body></html>
`

const textoEstorno = `Seu estorno foi concluído

Não foi possível confirmar os assentos do pedido {{.Codigo}}, e o valor de {{.Total}} foi devolvido ao seu cartão.
O prazo para aparecer na fatura depende do banco emissor. Nenhum ingresso deste pedido é válido.
`

var (
	tplEstornoHTML  = sync.OnceValue(func() *htmltemplate.Template { return htmltemplate.Must(htmltemplate.New("eh").Parse(htmlEstorno)) })
	tplEstornoTexto = sync.OnceValue(func() *texttemplate.Template { return texttemplate.Must(texttemplate.New("et").Parse(textoEstorno)) })
)

// Fake é o EmailSender em memória (CI, dev e load-test — o Resend grátis
// aguenta 100/dia). Recusado em produção pelo boot (task 0029). Seguro para
// uso concorrente; falha programável.
type Fake struct {
	mu         sync.Mutex
	enviadas   []Mensagem
	falharAte  int
	tentativas int
}

// NovoFake cria o fake sem falhas.
func NovoFake() *Fake { return &Fake{} }

// FalharPrimeiras faz as n primeiras tentativas falharem (erro transitório).
func (f *Fake) FalharPrimeiras(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.falharAte = n
}

// Enviar grava a mensagem (ou falha, se programado).
func (f *Fake) Enviar(_ context.Context, m Mensagem) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tentativas++
	if f.tentativas <= f.falharAte {
		return fmt.Errorf("notificacao: falha programada na tentativa %d", f.tentativas)
	}
	f.enviadas = append(f.enviadas, m)
	return nil
}

// Enviadas devolve uma cópia das mensagens enviadas.
func (f *Fake) Enviadas() []Mensagem {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Mensagem(nil), f.enviadas...)
}

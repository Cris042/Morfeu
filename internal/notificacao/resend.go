package notificacao

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Classificação dos erros de envio (PRD 0029): o consumidor decide pela
// classe — permanente e cota vão à DLQ; o resto volta à fila.
var (
	// ErrEnvioPermanente: o provedor recusou a mensagem (4xx de validação,
	// chave inválida…) — repetir não adianta.
	ErrEnvioPermanente = errors.New("notificacao: envio recusado pelo provedor")
	// ErrCotaEsgotada: cota diária/mensal do plano grátis (Resend: 100/dia).
	// DLQ + métrica; o operador faz o replay no dia seguinte.
	ErrCotaEsgotada = errors.New("notificacao: cota de envio do provedor esgotada")
)

// Limites do adapter (refinamento E7, SRE): timeout por chamada, sem retry
// interno (a redelivery do consumidor já repete) e corpo de resposta limitado.
const (
	resendURLPadrao   = "https://api.resend.com"
	timeoutResend     = 5 * time.Second
	limiteRespostaAPI = 64 << 10
)

// ConfigResend configura o adapter. URL só em teste (servidor falso).
type ConfigResend struct {
	Chave     string
	Remetente string
	URL       string
}

// Resend é o EmailSender real (HTTP cru — sem SDK, refinamento E7).
type Resend struct {
	cfg  ConfigResend
	http *http.Client
}

// NovoResend exige chave e remetente.
func NovoResend(cfg ConfigResend) (*Resend, error) {
	if cfg.Chave == "" || cfg.Remetente == "" {
		return nil, errors.New("notificacao: Resend exige chave e remetente")
	}
	if cfg.URL == "" {
		cfg.URL = resendURLPadrao
	}
	return &Resend{cfg: cfg, http: &http.Client{Timeout: timeoutResend}}, nil
}

type anexoResend struct {
	Filename    string `json:"filename"`
	Content     string `json:"content"`
	ContentID   string `json:"content_id,omitempty"`
	ContentType string `json:"content_type,omitempty"`
}

type emailResend struct {
	From        string        `json:"from"`
	To          []string      `json:"to"`
	Subject     string        `json:"subject"`
	HTML        string        `json:"html"`
	Text        string        `json:"text"`
	Attachments []anexoResend `json:"attachments,omitempty"`
}

// Enviar faz POST /emails com a chave de idempotência da mensagem (o
// Resend descarta a repetição por 24 h). Erros nunca carregam o corpo da
// resposta (pode ecoar o destinatário) — só status e o nome do erro.
func (r *Resend) Enviar(ctx context.Context, m Mensagem) error {
	corpo := emailResend{From: r.cfg.Remetente, To: []string{m.Para}, Subject: m.Assunto, HTML: m.HTML, Text: m.Texto}
	for _, a := range m.Anexos {
		corpo.Attachments = append(corpo.Attachments, anexoResend{
			Filename: a.Nome, Content: base64.StdEncoding.EncodeToString(a.Conteudo), ContentID: a.ContentID, ContentType: a.Tipo,
		})
	}
	b, err := json.Marshal(corpo)
	if err != nil {
		return fmt.Errorf("%w: serializar: %w", ErrEnvioPermanente, err)
	}
	ctx, cancel := context.WithTimeout(ctx, timeoutResend)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.URL+"/emails", bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("%w: requisição: %w", ErrEnvioPermanente, err)
	}
	req.Header.Set("Authorization", "Bearer "+r.cfg.Chave)
	req.Header.Set("Content-Type", "application/json")
	if m.ChaveIdempotencia != "" {
		req.Header.Set("Idempotency-Key", m.ChaveIdempotencia)
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return fmt.Errorf("notificacao: Resend inacessível: %w", semURL(err))
	}
	defer func() { _ = resp.Body.Close() }()
	return classificar(resp)
}

// classificar mapeia a resposta (refinamento E7): 2xx ok; 429 de cota →
// ErrCotaEsgotada; 429 de taxa, 409 concorrente, 5xx → transitório; demais
// 4xx → ErrEnvioPermanente.
func classificar(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, limiteRespostaAPI))
		return nil
	}
	var e struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, limiteRespostaAPI)).Decode(&e)
	nome := nomeSeguro(e.Name)
	desc := fmt.Sprintf("Resend %d %s", resp.StatusCode, nome)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests && (nome == "daily_quota_exceeded" || nome == "monthly_quota_exceeded"):
		return fmt.Errorf("%w: %s", ErrCotaEsgotada, desc)
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= http.StatusInternalServerError,
		resp.StatusCode == http.StatusConflict && nome == "concurrent_idempotent_requests":
		return fmt.Errorf("notificacao: falha transitória: %s", desc)
	default:
		return fmt.Errorf("%w: %s", ErrEnvioPermanente, desc)
	}
}

// nomeSeguro aceita só o formato dos códigos de erro do Resend ([a-z_],
// até 64): o texto vai para o log e nada vindo de fora entra sem filtro.
func nomeSeguro(n string) string {
	if n == "" || len(n) > 64 {
		return "desconhecido"
	}
	for _, r := range n {
		if (r < 'a' || r > 'z') && r != '_' {
			return "desconhecido"
		}
	}
	return n
}

// semURL tira a URL do erro de rede (evita repetir host/caminho no log).
func semURL(err error) error {
	var ue interface{ Unwrap() error }
	if errors.As(err, &ue) && ue.Unwrap() != nil {
		return ue.Unwrap()
	}
	return err
}

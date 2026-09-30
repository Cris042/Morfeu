package pagamento

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86/webhook"
)

// toleranciaWebhook é a janela do timestamp assinado (padrão do Stripe, 5
// min — refinamento E6, security: não afrouxar).
const toleranciaWebhook = 300 * time.Second

// ErrAssinaturaInvalida cobre assinatura ausente, malformada, de outro
// segredo, corpo adulterado ou timestamp fora da tolerância. O handler
// responde 400 sem detalhe.
var ErrAssinaturaInvalida = errors.New("pagamento: assinatura do webhook inválida")

// TipoEvento é o que o pedido precisa saber de um evento do gateway.
type TipoEvento string

// Tipos tratados; qualquer outro evento vira Ignorado (2xx sem efeito).
const (
	PagamentoAprovado TipoEvento = "pagamento_aprovado" // payment_intent.succeeded
	PagamentoRecusado TipoEvento = "pagamento_recusado" // payment_intent.payment_failed (o cliente pode tentar de novo)
	Ignorado          TipoEvento = "ignorado"
)

// EventoPagamento é o evento já verificado, reduzido aos campos que o pivô
// cruza com o pedido persistido (doc.md §14.2).
type EventoPagamento struct {
	ID            string
	Tipo          TipoEvento
	IntencaoID    string
	PedidoID      uuid.UUID // do metadata; uuid.Nil se ausente ou inválido
	ValorCentavos int64
	Moeda         string
}

// Webhook verifica e interpreta eventos do Stripe. Única implementação: nos
// testes e no modo fake a assinatura é verificada do mesmo jeito, com um
// segredo de teste — o caminho de verificação nunca é desligado.
type Webhook struct {
	segredo string
	agora   func() time.Time
}

// NovoWebhook cria o verificador; segredo vazio é recusado.
func NovoWebhook(segredo string) (*Webhook, error) {
	if segredo == "" {
		return nil, errors.New("pagamento: segredo do webhook ausente")
	}
	return &Webhook{segredo: segredo, agora: time.Now}, nil
}

// intencaoMinima é o parse próprio dos campos usados: a versão de API do
// evento pode diferir da do SDK sem afetar estes campos estáveis.
type intencaoMinima struct {
	ID       string            `json:"id"`
	Amount   int64             `json:"amount"`
	Currency string            `json:"currency"`
	Metadata map[string]string `json:"metadata"`
}

// Verificar confere a assinatura sobre o CORPO BRUTO (antes de qualquer
// parse) e devolve o evento interpretado.
func (w *Webhook) Verificar(corpo []byte, assinatura string) (EventoPagamento, error) {
	ev, err := webhook.ConstructEventWithOptions(corpo, assinatura, w.segredo, webhook.ConstructEventOptions{
		Tolerance:                toleranciaWebhook,
		IgnoreAPIVersionMismatch: true,
	})
	if err != nil || w.noFuturo(assinatura) {
		return EventoPagamento{}, ErrAssinaturaInvalida
	}
	out := EventoPagamento{ID: ev.ID, Tipo: Ignorado}
	switch string(ev.Type) { // só dois tipos importam; o resto é ignorado
	case "payment_intent.succeeded":
		out.Tipo = PagamentoAprovado
	case "payment_intent.payment_failed":
		out.Tipo = PagamentoRecusado
	default:
		return out, nil
	}
	var pi intencaoMinima
	if ev.Data == nil || json.Unmarshal(ev.Data.Raw, &pi) != nil {
		return EventoPagamento{}, ErrAssinaturaInvalida
	}
	out.IntencaoID, out.ValorCentavos, out.Moeda = pi.ID, pi.Amount, pi.Currency
	if id, err := uuid.Parse(pi.Metadata[metadadoPedido]); err == nil {
		out.PedidoID = id
	}
	return out, nil
}

// noFuturo recusa timestamp assinado além da tolerância no futuro: o SDK só
// recusa o antigo, e um "t" futuro alongaria a janela de replay.
func (w *Webhook) noFuturo(assinatura string) bool {
	for _, parte := range strings.Split(assinatura, ",") {
		if v, ok := strings.CutPrefix(parte, "t="); ok {
			t, err := strconv.ParseInt(v, 10, 64)
			return err != nil || time.Unix(t, 0).After(w.agora().Add(toleranciaWebhook))
		}
	}
	return true
}

// EventoAprovadoDeTeste monta e ASSINA um payment_intent.succeeded com o
// segredo deste webhook — só para a rota de teste do gateway fake (PRD 0031),
// que o passa pelo mesmo Verificar do webhook real. Nunca registrada em
// produção (o boot recusa gateway fake).
func (w *Webhook) EventoAprovadoDeTeste(eventoID, intencaoID string, pedidoID uuid.UUID, valorCentavos int64, moeda string) (corpo []byte, assinatura string, err error) {
	corpo, err = json.Marshal(map[string]any{
		"id": eventoID, "object": "event", "type": "payment_intent.succeeded",
		"data": map[string]any{"object": map[string]any{
			"id": intencaoID, "object": "payment_intent", "amount": valorCentavos, "currency": moeda,
			"metadata": map[string]string{metadadoPedido: pedidoID.String()},
		}},
	})
	if err != nil {
		return nil, "", err
	}
	assinado := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: corpo, Secret: w.segredo, Timestamp: w.agora()})
	return corpo, assinado.Header, nil
}

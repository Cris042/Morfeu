package pagamento

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/stripe/stripe-go/v86"
)

// Limites do adapter (refinamento E6, SRE): timeout por chamada; o SDK repete
// falhas de rede com a mesma Idempotency-Key (2 retries = até 3 tentativas) —
// erro já respondido pela API só é repetido no lock timeout, por decisão do
// próprio SDK; o breaker abre após 5 falhas seguidas e fica aberto 30 s.
const (
	timeoutStripe      = 5 * time.Second
	retriesStripe      = 2
	falhasParaAbrir    = 5
	breakerAbertoPor   = 30 * time.Second
	metadadoPedido     = "pedido_id"
	opCriarCobranca    = "criar_cobranca"
	resultadoOK        = "ok"
	resultadoFalha     = "falha"
	resultadoRecusado  = "recusado"
	resultadoBloqueado = "breaker_aberto"
)

// ErrChaveInvalida recusa chave ausente ou de produção: o projeto só opera o
// Stripe em modo de teste (doc.md §10, sandbox).
var ErrChaveInvalida = errors.New("pagamento: chave do Stripe ausente ou não é de teste (sk_test_/rk_test_)")

// ConfigStripe configura o adapter. URL só em teste (servidor falso).
type ConfigStripe struct {
	Chave string
	URL   string
	// Observar recebe cada chamada (op, resultado, duração) — métricas gateway_*.
	Observar func(op, resultado string, d time.Duration)
	// AoMudarBreaker recebe o estado do breaker (true = aberto).
	AoMudarBreaker func(aberto bool)
	Agora          func() time.Time
}

// Stripe é o adapter real do Gateway (PaymentIntent + Payment Element, ADR 0010).
type Stripe struct {
	sc      *stripe.Client
	cfg     ConfigStripe
	breaker *breaker
}

// ChaveDeTeste informa se a chave é de modo de teste do Stripe.
func ChaveDeTeste(chave string) bool {
	return strings.HasPrefix(chave, "sk_test_") || strings.HasPrefix(chave, "rk_test_")
}

// NovoStripe cria o adapter; recusa chave ausente ou de produção.
func NovoStripe(cfg ConfigStripe) (*Stripe, error) {
	if !ChaveDeTeste(cfg.Chave) {
		return nil, ErrChaveInvalida
	}
	if cfg.Agora == nil {
		cfg.Agora = time.Now
	}
	if cfg.Observar == nil {
		cfg.Observar = func(string, string, time.Duration) {}
	}
	if cfg.AoMudarBreaker == nil {
		cfg.AoMudarBreaker = func(bool) {}
	}
	backend := &stripe.BackendConfig{
		HTTPClient:        &http.Client{Timeout: timeoutStripe},
		MaxNetworkRetries: stripe.Int64(retriesStripe),
		// O SDK não loga nada: erros chegam a nós e são logados sem payload.
		LeveledLogger: &stripe.LeveledLogger{Level: stripe.LevelNull},
	}
	if cfg.URL != "" {
		backend.URL = stripe.String(cfg.URL)
	}
	sc := stripe.NewClient(cfg.Chave, stripe.WithBackends(stripe.NewBackendsWithConfig(backend)))
	return &Stripe{sc: sc, cfg: cfg, breaker: &breaker{aoMudar: cfg.AoMudarBreaker}}, nil
}

// CriarCobranca cria o PaymentIntent do pedido: valor e moeda do servidor,
// pedido_id no metadata (o webhook cruza) e a chave de idempotência do
// pedido (retry não duplica cobrança).
func (s *Stripe) CriarCobranca(ctx context.Context, c Cobranca) (Intencao, error) {
	inicio := s.cfg.Agora()
	if !s.breaker.permitir(inicio) {
		s.cfg.Observar(opCriarCobranca, resultadoBloqueado, 0)
		return Intencao{}, fmt.Errorf("%w: breaker aberto", ErrIndisponivel)
	}
	params := &stripe.PaymentIntentCreateParams{
		Amount:                  stripe.Int64(c.ValorCentavos),
		Currency:                stripe.String(c.Moeda),
		AutomaticPaymentMethods: &stripe.PaymentIntentCreateAutomaticPaymentMethodsParams{Enabled: stripe.Bool(true)},
	}
	params.AddMetadata(metadadoPedido, c.PedidoID.String())
	params.SetIdempotencyKey(c.ChaveIdempotencia)
	ctx, cancelar := context.WithTimeout(ctx, timeoutStripe*(retriesStripe+1))
	defer cancelar()
	pi, err := s.sc.V1PaymentIntents.Create(ctx, params)
	dur := s.cfg.Agora().Sub(inicio)
	if err != nil {
		transitoria := falhaTransitoria(err)
		s.breaker.registrar(s.cfg.Agora(), !transitoria)
		resultado := resultadoRecusado
		if transitoria {
			resultado = resultadoFalha
		}
		s.cfg.Observar(opCriarCobranca, resultado, dur)
		return Intencao{}, fmt.Errorf("%w: %s", ErrIndisponivel, descreverErro(err))
	}
	s.breaker.registrar(s.cfg.Agora(), true)
	s.cfg.Observar(opCriarCobranca, resultadoOK, dur)
	return Intencao{ID: pi.ID, SegredoCliente: pi.ClientSecret}, nil
}

// falhaTransitoria: rede, timeout, 429 e 5xx contam para o breaker; 4xx de
// requisição inválida é defeito nosso e não abre o circuito.
func falhaTransitoria(err error) bool {
	var se *stripe.Error
	if !errors.As(err, &se) {
		return true
	}
	return se.HTTPStatusCode == http.StatusTooManyRequests || se.HTTPStatusCode >= http.StatusInternalServerError || se.HTTPStatusCode == 0
}

// descreverErro devolve só tipo/código/status do Stripe — nunca o corpo.
func descreverErro(err error) string {
	var se *stripe.Error
	if errors.As(err, &se) {
		return fmt.Sprintf("stripe %d %s/%s", se.HTTPStatusCode, se.Type, se.Code)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return err.Error()
	}
	return "falha de rede"
}

// breaker é o circuit breaker do gateway — o ÚNICO do sistema (doc.md §13):
// abre com falhasParaAbrir falhas transitórias seguidas e recusa na hora por
// breakerAbertoPor; depois deixa passar (meio-aberto): um sucesso fecha, uma
// falha reabre de imediato.
type breaker struct {
	mu        sync.Mutex
	falhas    int
	abertoAte time.Time // zero = fechado; não zero = aberto ou meio-aberto
	aberto    bool      // último estado informado a aoMudar
	aoMudar   func(aberto bool)
}

func (b *breaker) permitir(agora time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !agora.Before(b.abertoAte)
}

// registrar contabiliza uma chamada (ok = sucesso ou erro não transitório).
func (b *breaker) registrar(agora time.Time, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case ok:
		b.falhas = 0
		b.abertoAte = time.Time{}
	case !b.abertoAte.IsZero() || b.falhas+1 >= falhasParaAbrir:
		b.falhas = 0
		b.abertoAte = agora.Add(breakerAbertoPor)
	default:
		b.falhas++
	}
	if aberto := agora.Before(b.abertoAte); aberto != b.aberto {
		b.aberto = aberto
		b.aoMudar(aberto)
	}
}

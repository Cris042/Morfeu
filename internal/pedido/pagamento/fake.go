package pagamento

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Fake é o gateway em memória (refinamento E6, QA): único e programável —
// o mesmo serve aos testes (falha na N-ésima chamada, latência, registro das
// chamadas, estado de cada cobrança) e ao modo load-test do E12 (Stripe test
// aguenta ~25 req/s). Nunca usado em produção (o boot recusa). Seguro para
// uso concorrente.
type Fake struct {
	mu             sync.Mutex
	chamadas       []Cobranca
	falharEm       map[int]bool
	latencia       time.Duration
	cobrancas      map[string]*cobrancaFake
	estornos       []string // chaves de idempotência recebidas, na ordem
	estornosFalhos int      // próximos estornos que falham
	cancelamentos  []string
}

type cobrancaFake struct {
	valor  int64
	moeda  string
	estado EstadoCobranca
}

// ErrCobrancaDesconhecida: a cobrança não foi criada neste gateway.
var ErrCobrancaDesconhecida = errors.New("pagamento: cobrança desconhecida")

// NovoFake cria o fake sem falhas nem latência.
func NovoFake() *Fake {
	return &Fake{falharEm: map[int]bool{}, cobrancas: map[string]*cobrancaFake{}}
}

// FalharNa faz a n-ésima criação de cobrança (1-based) devolver ErrIndisponivel.
func (f *Fake) FalharNa(n ...int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, i := range n {
		f.falharEm[i] = true
	}
}

// FalharEstornos faz os próximos n estornos devolverem ErrIndisponivel.
func (f *Fake) FalharEstornos(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.estornosFalhos = n
}

// ComLatencia atrasa cada criação (respeitando o cancelamento do contexto).
func (f *Fake) ComLatencia(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.latencia = d
}

// Aprovar simula o cliente pagando a cobrança (sem webhook — é o cenário do
// webhook perdido que a reconciliação cobre).
func (f *Fake) Aprovar(intencaoID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.cobrancas[intencaoID]; ok {
		c.estado = CobrancaAprovada
	}
}

// Chamadas devolve uma cópia das cobranças recebidas, na ordem.
func (f *Fake) Chamadas() []Cobranca {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Cobranca(nil), f.chamadas...)
}

// Estornos devolve as chaves de idempotência dos estornos recebidos.
func (f *Fake) Estornos() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.estornos...)
}

// Cancelamentos devolve as cobranças canceladas com sucesso.
func (f *Fake) Cancelamentos() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cancelamentos...)
}

// CriarCobranca registra a chamada e devolve uma intenção determinística
// derivada do pedido (o mesmo pedido gera o mesmo id — idempotente como o
// gateway real com a mesma chave).
func (f *Fake) CriarCobranca(ctx context.Context, c Cobranca) (Intencao, error) {
	f.mu.Lock()
	f.chamadas = append(f.chamadas, c)
	n := len(f.chamadas)
	falhar, latencia := f.falharEm[n], f.latencia
	f.mu.Unlock()
	if latencia > 0 {
		select {
		case <-time.After(latencia):
		case <-ctx.Done():
			return Intencao{}, fmt.Errorf("%w: %w", ErrIndisponivel, ctx.Err())
		}
	}
	if falhar {
		return Intencao{}, fmt.Errorf("%w: falha programada na chamada %d", ErrIndisponivel, n)
	}
	id := "pi_fake_" + c.PedidoID.String()
	f.mu.Lock()
	if _, ok := f.cobrancas[id]; !ok {
		f.cobrancas[id] = &cobrancaFake{valor: c.ValorCentavos, moeda: c.Moeda, estado: CobrancaPendente}
	}
	f.mu.Unlock()
	return Intencao{ID: id, SegredoCliente: id + "_secret_fake"}, nil
}

// ConsultarCobranca devolve o estado da cobrança.
func (f *Fake) ConsultarCobranca(_ context.Context, intencaoID string) (Situacao, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.cobrancas[intencaoID]
	if !ok {
		return Situacao{}, ErrCobrancaDesconhecida
	}
	return Situacao{Estado: c.estado, ValorCentavos: c.valor, Moeda: c.moeda}, nil
}

// CancelarCobranca cancela a cobrança pendente; aprovada não é cancelada.
func (f *Fake) CancelarCobranca(_ context.Context, intencaoID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.cobrancas[intencaoID]
	switch {
	case !ok:
		return ErrCobrancaDesconhecida
	case c.estado == CobrancaAprovada:
		return fmt.Errorf("%w: cobrança já aprovada", ErrIndisponivel)
	}
	c.estado = CobrancaCancelada
	f.cancelamentos = append(f.cancelamentos, intencaoID)
	return nil
}

// Estornar registra o estorno (a mesma chave repetida é aceita — idempotente
// como o gateway real).
func (f *Fake) Estornar(_ context.Context, intencaoID, chave string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.estornos = append(f.estornos, chave)
	if f.estornosFalhos > 0 {
		f.estornosFalhos--
		return fmt.Errorf("%w: estorno falhou (programado)", ErrIndisponivel)
	}
	if _, ok := f.cobrancas[intencaoID]; !ok {
		return ErrCobrancaDesconhecida
	}
	return nil
}

// RecuperarSegredo devolve o mesmo segredo determinístico da criação.
func (f *Fake) RecuperarSegredo(_ context.Context, intencaoID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.cobrancas[intencaoID]; !ok {
		return "", ErrCobrancaDesconhecida
	}
	return intencaoID + "_secret_fake", nil
}

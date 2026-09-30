package pagamento

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Fake é o gateway em memória (refinamento E6, QA): único e programável —
// o mesmo serve aos testes (falha na N-ésima chamada, latência, registro das
// chamadas) e ao modo load-test do E12 (Stripe test aguenta ~25 req/s). Nunca
// usado em produção (o main recusa — task 0024). Seguro para uso concorrente.
type Fake struct {
	mu       sync.Mutex
	chamadas []Cobranca
	falharEm map[int]bool
	latencia time.Duration
}

// NovoFake cria o fake sem falhas nem latência.
func NovoFake() *Fake { return &Fake{falharEm: map[int]bool{}} }

// FalharNa faz a n-ésima chamada (1-based) devolver ErrIndisponivel.
func (f *Fake) FalharNa(n ...int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, i := range n {
		f.falharEm[i] = true
	}
}

// ComLatencia atrasa cada chamada (respeitando o cancelamento do contexto).
func (f *Fake) ComLatencia(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.latencia = d
}

// Chamadas devolve uma cópia das cobranças recebidas, na ordem.
func (f *Fake) Chamadas() []Cobranca {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Cobranca(nil), f.chamadas...)
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
	return Intencao{ID: id, SegredoCliente: id + "_secret_fake"}, nil
}

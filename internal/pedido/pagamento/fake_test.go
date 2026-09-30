package pagamento

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestFake(t *testing.T) {
	f := NovoFake()
	f.FalharNa(2)
	c := Cobranca{PedidoID: uuid.New(), ValorCentavos: 6000, Moeda: "brl", ChaveIdempotencia: "k"}

	primeira, err := f.CriarCobranca(context.Background(), c)
	if err != nil || primeira.ID == "" || primeira.SegredoCliente == "" {
		t.Fatalf("1ª: %+v %v", primeira, err)
	}
	if _, err := f.CriarCobranca(context.Background(), c); !errors.Is(err, ErrIndisponivel) {
		t.Fatalf("2ª deveria falhar: %v", err)
	}
	terceira, err := f.CriarCobranca(context.Background(), c)
	if err != nil || terceira != primeira {
		t.Fatalf("mesmo pedido deveria dar a mesma intenção: %+v × %+v (%v)", terceira, primeira, err)
	}
	if got := f.Chamadas(); len(got) != 3 || got[0] != c {
		t.Fatalf("chamadas: %+v", got)
	}
}

func TestFake_LatenciaRespeitaContexto(t *testing.T) {
	f := NovoFake()
	f.ComLatencia(time.Hour)
	ctx, cancelar := context.WithCancel(context.Background())
	cancelar()
	if _, err := f.CriarCobranca(ctx, Cobranca{PedidoID: uuid.New()}); !errors.Is(err, ErrIndisponivel) || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

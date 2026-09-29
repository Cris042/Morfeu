package reserva

import (
	"regexp"
	"sort"
	"time"

	"github.com/google/uuid"
)

// Regras da trava (ADR 0008; decisões do usuário no refinamento E4).
const (
	TTLHold         = 10 * time.Minute // prazo de um hold novo
	ExtensaoHold    = 10 * time.Minute // acréscimo da única extensão
	MaxHoldsPorDono = 6                // vivos, somando todas as sessões
	maxExtensoes    = 1
	maxAssentosLote = MaxHoldsPorDono
)

// AssentoCodigo é o código de uma cadeira ("F7"): fileira A–Z + coluna 1–99.
// Existir no layout da sala é verificado pela porta do sessao.
type AssentoCodigo string

var formatoAssento = regexp.MustCompile(`^[A-Z][1-9][0-9]?$`)

// NovoAssentoCodigo valida o formato.
func NovoAssentoCodigo(s string) (AssentoCodigo, error) {
	if !formatoAssento.MatchString(s) {
		return "", &ErroValidacao{Campos: []string{"assentos"}}
	}
	return AssentoCodigo(s), nil
}

// StatusHold é o ciclo de vida do hold. Só 'ativo' ocupa o índice único.
type StatusHold string

// Estados do hold (espelham o CHECK da migration 009).
const (
	StatusAtivo      StatusHold = "ativo"
	StatusLiberado   StatusHold = "liberado"
	StatusExpirado   StatusHold = "expirado"
	StatusConvertido StatusHold = "convertido"
)

// Hold é o aggregate da trava de um assento. Primeira linha de defesa (em
// memória); o índice parcial e os UPDATE com guarda são a última (ADR 0005).
type Hold struct {
	id              uuid.UUID
	sessaoID        int64
	assento         AssentoCodigo
	expiraEm        time.Time
	extensoesUsadas int
}

// reconstituir monta o aggregate a partir do repositório (hold ativo).
func reconstituir(id uuid.UUID, sessaoID int64, assento string, expiraEm time.Time, extensoes int) Hold {
	return Hold{id: id, sessaoID: sessaoID, assento: AssentoCodigo(assento), expiraEm: expiraEm, extensoesUsadas: extensoes}
}

// ID do hold.
func (h Hold) ID() uuid.UUID { return h.id }

// SessaoID da sessão travada.
func (h Hold) SessaoID() int64 { return h.sessaoID }

// Assento travado.
func (h Hold) Assento() AssentoCodigo { return h.assento }

// ExpiraEm é o fim do prazo.
func (h Hold) ExpiraEm() time.Time { return h.expiraEm }

// ExtensoesUsadas já consumidas (0 ou 1).
func (h Hold) ExtensoesUsadas() int { return h.extensoesUsadas }

// Vivo informa se o hold ainda trava o assento: o prazo é exclusivo — no
// instante exato de expiraEm o hold já pode ser roubado (RN02).
func (h Hold) Vivo(agora time.Time) bool { return agora.Before(h.expiraEm) }

// Estender aplica a única extensão (+10 min sobre o prazo atual).
func (h Hold) Estender(agora time.Time) (Hold, error) {
	if !h.Vivo(agora) {
		return Hold{}, ErrHoldNaoEncontrado
	}
	if h.extensoesUsadas >= maxExtensoes {
		return Hold{}, ErrExtensaoEsgotada
	}
	h.expiraEm = h.expiraEm.Add(ExtensaoHold)
	h.extensoesUsadas++
	return h, nil
}

// Lote é o pedido de trava validado em memória: 1–6 códigos de formato
// válido, sem repetição, em ordem crescente — a ordem global que evita espera
// circular entre lotes sobrepostos (ADR 0008).
type Lote struct {
	assentos []AssentoCodigo
}

// NovoLote valida e ordena os códigos pedidos.
func NovoLote(codigos []string) (Lote, error) {
	if len(codigos) == 0 || len(codigos) > maxAssentosLote {
		return Lote{}, &ErroValidacao{Campos: []string{"assentos"}}
	}
	vistos := make(map[AssentoCodigo]bool, len(codigos))
	out := make([]AssentoCodigo, 0, len(codigos))
	for _, c := range codigos {
		a, err := NovoAssentoCodigo(c)
		if err != nil || vistos[a] {
			return Lote{}, &ErroValidacao{Campos: []string{"assentos"}}
		}
		vistos[a] = true
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return Lote{assentos: out}, nil
}

// Assentos do lote, em ordem crescente.
func (l Lote) Assentos() []AssentoCodigo { return append([]AssentoCodigo(nil), l.assentos...) }

// ContidoEm verifica o lote contra os códigos reais da sala (vãos e posições
// fora da grade não existem).
func (l Lote) ContidoEm(codigosDaSala []string) error {
	existe := make(map[string]bool, len(codigosDaSala))
	for _, c := range codigosDaSala {
		existe[c] = true
	}
	for _, a := range l.assentos {
		if !existe[string(a)] {
			return &ErroValidacao{Campos: []string{"assentos"}}
		}
	}
	return nil
}

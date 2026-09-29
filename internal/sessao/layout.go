package sessao

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// Limites do layout (RF02 do PRD 0013; teto de segurança do refinamento E3).
const (
	maxFileiras     = 26 // letras A–Z
	maxColunas      = 50
	maxBytesLayout  = 64 << 10
	primeiraFileira = 'A'
)

// Posicao é uma posição na grade (fileira em letra, coluna a partir de 1).
type Posicao struct {
	Fileira string `json:"fileira"`
	Coluna  int    `json:"coluna"`
}

// Layout é o template JSON da sala: grade fileiras × colunas, com vãos
// (corredores/ausência de cadeira) e assentos PCD. Nunca editor visual.
type Layout struct {
	Fileiras int       `json:"fileiras"`
	Colunas  int       `json:"colunas"`
	Vaos     []Posicao `json:"vaos"`
	PCD      []Posicao `json:"pcd"`
}

// Assento é uma cadeira real da sala (vãos não geram assento).
type Assento struct {
	Codigo  string `json:"codigo"`
	Fileira string `json:"fileira"`
	Coluna  int    `json:"coluna"`
	PCD     bool   `json:"pcd"`
}

// LerLayout decodifica e valida o JSON do layout com schema estrito (chave
// desconhecida é erro) e teto de tamanho — entrada do operador tratada como
// não confiável (refinamento E3, security).
func LerLayout(bruto []byte) (Layout, error) {
	if len(bruto) == 0 || len(bruto) > maxBytesLayout {
		return Layout{}, &ErroValidacao{Campos: []string{"layout"}}
	}
	dec := json.NewDecoder(bytes.NewReader(bruto))
	dec.DisallowUnknownFields()
	var l Layout
	if err := dec.Decode(&l); err != nil || dec.More() {
		return Layout{}, &ErroValidacao{Campos: []string{"layout"}}
	}
	if err := l.Validar(); err != nil {
		return Layout{}, err
	}
	return l, nil
}

// Validar aplica as regras da grade: limites, posições dentro da grade, sem
// repetição, PCD nunca em vão.
func (l Layout) Validar() error {
	if l.Fileiras < 1 || l.Fileiras > maxFileiras || l.Colunas < 1 || l.Colunas > maxColunas {
		return &ErroValidacao{Campos: []string{"layout"}}
	}
	vaos := map[string]bool{}
	for _, p := range l.Vaos {
		c, ok := l.codigo(p)
		if !ok || vaos[c] {
			return &ErroValidacao{Campos: []string{"layout.vaos"}}
		}
		vaos[c] = true
	}
	pcd := map[string]bool{}
	for _, p := range l.PCD {
		c, ok := l.codigo(p)
		if !ok || pcd[c] || vaos[c] {
			return &ErroValidacao{Campos: []string{"layout.pcd"}}
		}
		pcd[c] = true
	}
	return nil
}

// Assentos devolve as cadeiras da sala em ordem determinística (fileira A→Z,
// coluna 1→N), sem os vãos. É a fonte dos códigos de assento do E4 (holds).
func (l Layout) Assentos() []Assento {
	vaos := l.conjunto(l.Vaos)
	pcd := l.conjunto(l.PCD)
	out := make([]Assento, 0, l.Fileiras*l.Colunas-len(vaos))
	for f := 0; f < l.Fileiras; f++ {
		fileira := string(rune(primeiraFileira + f))
		for c := 1; c <= l.Colunas; c++ {
			codigo := fileira + strconv.Itoa(c)
			if vaos[codigo] {
				continue
			}
			out = append(out, Assento{Codigo: codigo, Fileira: fileira, Coluna: c, PCD: pcd[codigo]})
		}
	}
	return out
}

// codigo valida a posição contra a grade e devolve o código ("F7").
func (l Layout) codigo(p Posicao) (string, bool) {
	if len(p.Fileira) != 1 {
		return "", false
	}
	f := rune(p.Fileira[0]) - primeiraFileira
	if f < 0 || int(f) >= l.Fileiras || p.Coluna < 1 || p.Coluna > l.Colunas {
		return "", false
	}
	return fmt.Sprintf("%s%d", p.Fileira, p.Coluna), true
}

func (l Layout) conjunto(ps []Posicao) map[string]bool {
	m := make(map[string]bool, len(ps))
	for _, p := range ps {
		if c, ok := l.codigo(p); ok {
			m[c] = true
		}
	}
	return m
}

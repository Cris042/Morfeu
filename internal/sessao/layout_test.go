package sessao

import (
	"errors"
	"strings"
	"testing"
)

// TestLerLayout cobre CA02: um caso por regra de validação.
func TestLerLayout(t *testing.T) {
	validos := []string{
		`{"fileiras":1,"colunas":1,"vaos":[],"pcd":[]}`,
		`{"fileiras":26,"colunas":50,"vaos":[{"fileira":"Z","coluna":50}],"pcd":[{"fileira":"A","coluna":1}]}`,
		`{"fileiras":3,"colunas":4}`,
	}
	for _, j := range validos {
		if _, err := LerLayout([]byte(j)); err != nil {
			t.Errorf("layout válido rejeitado %s: %v", j, err)
		}
	}

	invalidos := map[string]string{
		"vazio":              ``,
		"não é JSON":         `{"fileiras":`,
		"chave desconhecida": `{"fileiras":2,"colunas":2,"script":"x"}`,
		"dois objetos":       `{"fileiras":2,"colunas":2}{"fileiras":1,"colunas":1}`,
		"0 fileiras":         `{"fileiras":0,"colunas":2}`,
		"27 fileiras":        `{"fileiras":27,"colunas":2}`,
		"51 colunas":         `{"fileiras":2,"colunas":51}`,
		"vão fora da grade":  `{"fileiras":2,"colunas":2,"vaos":[{"fileira":"C","coluna":1}]}`,
		"coluna 0":           `{"fileiras":2,"colunas":2,"vaos":[{"fileira":"A","coluna":0}]}`,
		"fileira minúscula":  `{"fileiras":2,"colunas":2,"vaos":[{"fileira":"a","coluna":1}]}`,
		"fileira dupla":      `{"fileiras":2,"colunas":2,"vaos":[{"fileira":"AB","coluna":1}]}`,
		"vão duplicado":      `{"fileiras":2,"colunas":2,"vaos":[{"fileira":"A","coluna":1},{"fileira":"A","coluna":1}]}`,
		"PCD duplicado":      `{"fileiras":2,"colunas":2,"pcd":[{"fileira":"B","coluna":2},{"fileira":"B","coluna":2}]}`,
		"PCD em vão":         `{"fileiras":2,"colunas":2,"vaos":[{"fileira":"A","coluna":1}],"pcd":[{"fileira":"A","coluna":1}]}`,
		"acima de 64 KB":     `{"fileiras":2,"colunas":2,"vaos":[` + strings.Repeat(`{"fileira":"A","coluna":1},`, 3000) + `{"fileira":"A","coluna":1}]}`,
	}
	for nome, j := range invalidos {
		_, err := LerLayout([]byte(j))
		if !errors.Is(err, ErrDadosInvalidos) {
			t.Errorf("%s: esperava erro de validação, veio %v", nome, err)
		}
	}
}

// TestAssentos: ordem determinística, vãos não geram assento, PCD marcado.
func TestAssentos(t *testing.T) {
	l, err := LerLayout([]byte(`{"fileiras":2,"colunas":3,"vaos":[{"fileira":"A","coluna":2}],"pcd":[{"fileira":"B","coluna":1}]}`))
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	var codigos []string
	var pcd []string
	for _, a := range l.Assentos() {
		codigos = append(codigos, a.Codigo)
		if a.PCD {
			pcd = append(pcd, a.Codigo)
		}
	}
	if got := strings.Join(codigos, ","); got != "A1,A3,B1,B2,B3" {
		t.Errorf("códigos = %s", got)
	}
	if strings.Join(pcd, ",") != "B1" {
		t.Errorf("PCD = %v", pcd)
	}
	for i := 0; i < 3; i++ {
		var de []string
		for _, a := range l.Assentos() {
			de = append(de, a.Codigo)
		}
		if strings.Join(de, ",") != "A1,A3,B1,B2,B3" {
			t.Fatal("códigos não são determinísticos")
		}
	}
}

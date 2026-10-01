//go:build integration
// +build integration

package observabilidade_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Contrato dashboards/alertas × métricas do app (task 0041, refinamento E10):
// toda métrica do Morfeu citada numa expressão PromQL precisa ser declarada
// no código (nome do instrumento OTel). Pega typo e métrica renomeada sem
// subir nenhum container.

// prefixosDoApp: métricas do próprio binário (as de exporters ficam de fora).
var prefixosDoApp = []string{"checkout_", "saga_", "cancelamentos_", "pedidos_", "gateway_", "morfeu_", "reserva_", "sessao_", "auditoria_", "limpeza_", "auth_", "tmdb_"}

var (
	nomeMetrica = regexp.MustCompile(`[a-z_][a-z0-9_]*`)
	instrumento = regexp.MustCompile(`(?:Counter|Histogram|Gauge|UpDownCounter)\("([a-z0-9_]+)"|contador\("([a-z0-9_]+)"`)
	sufixosProm = []string{"_bucket", "_count", "_sum"}
	// literal: valores de label (ex.: etapa="gateway_falhou") não são métricas.
	literal = regexp.MustCompile(`"[^"]*"`)
)

// declaradas lê os nomes de instrumentos do código Go (cmd/ e internal/).
func declaradas(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(raiz, dir), func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			for _, m := range instrumento.FindAllStringSubmatch(string(b), -1) {
				out[m[1]+m[2]] = true
			}
			return nil
		})
		if err != nil {
			t.Fatalf("varrer %s: %v", dir, err)
		}
	}
	if len(out) < 15 {
		t.Fatalf("só %d instrumentos encontrados — a varredura perdeu o alcance", len(out))
	}
	return out
}

func doApp(nome string) bool {
	for _, p := range prefixosDoApp {
		if strings.HasPrefix(nome, p) {
			return true
		}
	}
	return false
}

func conferir(t *testing.T, origem, expr string, metricas map[string]bool) {
	t.Helper()
	for _, tok := range nomeMetrica.FindAllString(literal.ReplaceAllString(expr, ""), -1) {
		if !doApp(tok) {
			continue
		}
		base := tok
		for _, s := range sufixosProm {
			base = strings.TrimSuffix(base, s)
		}
		if !metricas[base] {
			t.Errorf("%s cita %q, que o app não declara", origem, tok)
		}
	}
}

// coletarExprs junta todo campo "expr" de um JSON qualquer.
func coletarExprs(v any, out *[]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if s, ok := val.(string); ok && k == "expr" {
				*out = append(*out, s)
			}
			coletarExprs(val, out)
		}
	case []any:
		for _, val := range x {
			coletarExprs(val, out)
		}
	}
}

func TestContrato_DashboardsCitamMetricasDoApp(t *testing.T) {
	metricas := declaradas(t)
	arquivos, _ := filepath.Glob(filepath.Join(raiz, "configs/grafana/dashboards/*.json"))
	if len(arquivos) != 7 {
		t.Fatalf("esperava 7 dashboards (3 do E0d + 4 de negócio do E10), achei %d", len(arquivos))
	}
	uids := map[string]string{}
	for _, a := range arquivos {
		b, err := os.ReadFile(a)
		if err != nil {
			t.Fatal(err)
		}
		var d map[string]any
		if err := json.Unmarshal(b, &d); err != nil {
			t.Fatalf("%s: JSON inválido: %v", a, err)
		}
		uid, _ := d["uid"].(string)
		if uid == "" || uids[uid] != "" {
			t.Errorf("%s: uid vazio ou repetido (%q, já em %s)", a, uid, uids[uid])
		}
		uids[uid] = a
		var exprs []string
		coletarExprs(d, &exprs)
		for _, e := range exprs {
			conferir(t, filepath.Base(a), e, metricas)
		}
	}
}

func TestContrato_AlertasCitamMetricasDoApp(t *testing.T) {
	metricas := declaradas(t)
	b, err := os.ReadFile(filepath.Join(raiz, "configs/grafana/provisioning/alerting/alertas.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg any
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("alertas.yml inválido: %v", err)
	}
	var exprs []string
	coletarExprs(cfg, &exprs)
	if len(exprs) < 19 {
		t.Fatalf("só %d expressões nas regras", len(exprs))
	}
	for _, e := range exprs {
		conferir(t, "alertas.yml", e, metricas)
	}
}

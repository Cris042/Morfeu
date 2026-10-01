// Package infra valida a infraestrutura versionada do Morfeu (task 0042):
// compose de produção, preflight dos .env e roles do PostgreSQL. Os testes
// estáticos não precisam de containers nem de build tag.
package infra

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const caminhoComposeProd = "../../docker-compose.prod.yml"

// portasPermitidas: únicos serviços que publicam porta, e só estas (sempre em
// 127.0.0.1). AMQP (5672), PG, Redis e a stack de observabilidade ficam na
// rede interna.
var portasPermitidas = map[string][]string{
	"grafana":  {"3000"},
	"rabbitmq": {"15672"},
}

var (
	reNomeSegredo     = regexp.MustCompile(`(?i)(PASSWORD|_PASS$|SECRET|TOKEN)`)
	reObrigatoria     = regexp.MustCompile(`^\$\{[A-Za-z0-9_]+:\?[^}]*\}$`)
	servicosEsperados = []string{
		"postgres", "redis", "rabbitmq", "prometheus", "loki", "tempo", "alloy",
		"grafana", "node-exporter", "cadvisor", "postgres-exporter", "redis-exporter", "backup",
	}
)

type composeArquivo struct {
	Services map[string]map[string]any `yaml:"services"`
}

func lerCompose(t *testing.T, bruto []byte) composeArquivo {
	t.Helper()
	var c composeArquivo
	if err := yaml.Unmarshal(bruto, &c); err != nil {
		t.Fatalf("YAML inválido: %v", err)
	}
	return c
}

// portaPublicada descreve uma entrada de `ports:` (forma curta ou longa).
type portaPublicada struct {
	ip, alvo string
}

// lerPorta interpreta "ip:host:container/proto", "host:container",
// "container" e a forma longa (map com target/host_ip). Sem IP = 0.0.0.0.
func lerPorta(entrada any) (portaPublicada, error) {
	switch v := entrada.(type) {
	case string:
		s := v
		if i := strings.Index(s, "/"); i >= 0 {
			s = s[:i]
		}
		ip := ""
		if strings.HasPrefix(s, "[") { // [::1]:80:80
			fim := strings.Index(s, "]")
			if fim < 0 {
				return portaPublicada{}, fmt.Errorf("porta malformada %q", v)
			}
			ip, s = s[1:fim], strings.TrimPrefix(s[fim+1:], ":")
			partes := strings.Split(s, ":")
			return portaPublicada{ip: ip, alvo: partes[len(partes)-1]}, nil
		}
		partes := strings.Split(s, ":")
		if len(partes) >= 3 {
			ip = partes[0]
		}
		return portaPublicada{ip: ip, alvo: partes[len(partes)-1]}, nil
	case map[string]any:
		ip, _ := v["host_ip"].(string)
		return portaPublicada{ip: ip, alvo: fmt.Sprint(v["target"])}, nil
	}
	return portaPublicada{}, fmt.Errorf("formato de porta desconhecido: %T", entrada)
}

func loopback(ip string) bool { return ip == "127.0.0.1" || ip == "::1" }

// variaveisDeAmbiente normaliza `environment:` (map ou lista KEY=VALUE).
func variaveisDeAmbiente(s map[string]any) map[string]string {
	saida := map[string]string{}
	switch env := s["environment"].(type) {
	case map[string]any:
		for k, v := range env {
			saida[k] = fmt.Sprint(v)
		}
	case []any:
		for _, item := range env {
			k, v, _ := strings.Cut(fmt.Sprint(item), "=")
			saida[k] = v
		}
	}
	return saida
}

// violacoes aplica as regras do RF01/CA01 e devolve uma mensagem por problema
// (ordenada, para teste estável).
func violacoes(c composeArquivo) []string {
	var v []string
	for nome, s := range c.Services {
		if s["network_mode"] == "host" {
			v = append(v, nome+": network_mode host")
		}
		v = append(v, violacoesDePortas(nome, s)...)
		v = append(v, violacoesDeSenhas(nome, s)...)
		v = append(v, violacoesDeOperacao(nome, s)...)
	}
	sort.Strings(v)
	return v
}

func violacoesDePortas(nome string, s map[string]any) []string {
	var v []string
	portas, _ := s["ports"].([]any)
	for _, p := range portas {
		pp, err := lerPorta(p)
		if err != nil {
			v = append(v, nome+": "+err.Error())
			continue
		}
		if !loopback(pp.ip) {
			v = append(v, fmt.Sprintf("%s: porta %s publicada fora de 127.0.0.1", nome, pp.alvo))
		}
		if !contem(portasPermitidas[nome], pp.alvo) {
			v = append(v, fmt.Sprintf("%s: porta %s não deveria ser publicada", nome, pp.alvo))
		}
	}
	return v
}

func violacoesDeSenhas(nome string, s map[string]any) []string {
	var v []string
	for k, val := range variaveisDeAmbiente(s) {
		if reNomeSegredo.MatchString(k) && !reObrigatoria.MatchString(val) {
			v = append(v, fmt.Sprintf("%s: %s sem ${VAR:?}", nome, k))
		}
	}
	return v
}

func violacoesDeOperacao(nome string, s map[string]any) []string {
	var v []string
	log, _ := s["logging"].(map[string]any)
	opts, _ := log["options"].(map[string]any)
	if log["driver"] != "json-file" || fmt.Sprint(opts["max-size"]) != "10m" || fmt.Sprint(opts["max-file"]) != "3" {
		v = append(v, nome+": logging json-file 10m x 3 ausente")
	}
	if s["restart"] != "unless-stopped" {
		v = append(v, nome+": restart unless-stopped ausente")
	}
	if s["mem_limit"] == nil {
		v = append(v, nome+": mem_limit ausente")
	}
	return v
}

func contem(lista []string, x string) bool {
	for _, e := range lista {
		if e == x {
			return true
		}
	}
	return false
}

func TestComposeProd_Regras(t *testing.T) {
	bruto, err := os.ReadFile(caminhoComposeProd)
	if err != nil {
		t.Fatal(err)
	}
	c := lerCompose(t, bruto)
	for _, esperado := range servicosEsperados {
		if _, ok := c.Services[esperado]; !ok {
			t.Errorf("serviço %q ausente do compose de produção", esperado)
		}
	}
	if len(c.Services) != len(servicosEsperados) {
		t.Errorf("serviços = %d, esperado %d (app/Caddy ficam para a E0c-CD)", len(c.Services), len(servicosEsperados))
	}
	if v := violacoes(c); len(v) > 0 {
		t.Errorf("violações no docker-compose.prod.yml:\n  %s", strings.Join(v, "\n  "))
	}
}

// TestComposeProd_Redis: a senha não aparece nos argumentos e o healthcheck autentica.
func TestComposeProd_Redis(t *testing.T) {
	bruto, err := os.ReadFile(caminhoComposeProd)
	if err != nil {
		t.Fatal(err)
	}
	r := lerCompose(t, bruto).Services["redis"]
	cmd := fmt.Sprint(r["command"])
	if strings.Contains(cmd, "--requirepass") || !strings.Contains(cmd, "$$REDIS_PASSWORD") || !strings.Contains(cmd, "/tmp/redis.conf") {
		t.Errorf("command do Redis deve ler a senha do ambiente para um arquivo de config: %s", cmd)
	}
	hc, _ := r["healthcheck"].(map[string]any)
	if !strings.Contains(fmt.Sprint(hc["test"]), "REDISCLI_AUTH") {
		t.Errorf("healthcheck do Redis sem REDISCLI_AUTH: %v", hc["test"])
	}
	exp := variaveisDeAmbiente(lerCompose(t, bruto).Services["redis-exporter"])
	if exp["REDIS_PASSWORD"] == "" {
		t.Error("redis-exporter sem REDIS_PASSWORD")
	}
}

// servicoValido é um serviço que passa em todas as regras; os casos negativos
// acrescentam uma única violação (CA01: o teste precisa reprovar de verdade).
const servicoValido = `
    restart: unless-stopped
    mem_limit: 64m
    logging:
      driver: json-file
      options: {max-size: "10m", max-file: "3"}
`

func TestComposeProd_CasosNegativos(t *testing.T) {
	casos := []struct {
		nome, servicos, trecho string
	}{
		{"porta curta pública", "  postgres:\n    ports: [\"5432:5432\"]" + servicoValido, "fora de 127.0.0.1"},
		{"porta curta só do container", "  postgres:\n    ports: [\"5432\"]" + servicoValido, "fora de 127.0.0.1"},
		{"porta longa host_ip 0.0.0.0", "  postgres:\n    ports:\n      - {target: 5432, published: 5432, host_ip: 0.0.0.0}" + servicoValido, "fora de 127.0.0.1"},
		{"porta longa sem host_ip", "  postgres:\n    ports:\n      - {target: 5432, published: 5432}" + servicoValido, "fora de 127.0.0.1"},
		{"porta curta com 0.0.0.0", "  postgres:\n    ports: [\"0.0.0.0:5432:5432\"]" + servicoValido, "fora de 127.0.0.1"},
		{"loopback em serviço interno", "  prometheus:\n    ports: [\"127.0.0.1:9090:9090\"]" + servicoValido, "não deveria ser publicada"},
		{"AMQP em loopback", "  rabbitmq:\n    ports: [\"127.0.0.1:5672:5672\"]" + servicoValido, "não deveria ser publicada"},
		{"network_mode host", "  redis:\n    network_mode: host" + servicoValido, "network_mode host"},
		{"senha literal", "  postgres:\n    environment:\n      POSTGRES_PASSWORD: abc" + servicoValido, "sem ${VAR:?}"},
		{"senha com default", "  postgres:\n    environment:\n      - POSTGRES_PASSWORD=${POSTGRES_PASSWORD:-x}" + servicoValido, "sem ${VAR:?}"},
		{"sem logging", "  postgres:\n    restart: unless-stopped\n    mem_limit: 1g", "logging"},
		{"sem mem_limit", "  postgres:\n    restart: unless-stopped\n    logging: {driver: json-file, options: {max-size: 10m, max-file: 3}}", "mem_limit"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			v := violacoes(lerCompose(t, []byte("services:\n"+c.servicos+"\n")))
			if len(v) == 0 || !strings.Contains(strings.Join(v, ";"), c.trecho) {
				t.Errorf("a validação deveria reprovar com %q; violações: %v", c.trecho, v)
			}
		})
	}

	t.Run("compose válido sintético passa", func(t *testing.T) {
		ok := "  grafana:\n    ports: [\"127.0.0.1:3000:3000\"]\n    environment:\n      GF_SECURITY_ADMIN_PASSWORD: ${GF:?defina}" + servicoValido
		if v := violacoes(lerCompose(t, []byte("services:\n"+ok+"\n"))); len(v) != 0 {
			t.Errorf("violações inesperadas: %v", v)
		}
	})
}

// TestComposeProd_Backup: o serviço de backup (task 0043) usa o role
// morfeu_backup, recebe só a chave PÚBLICA e as credenciais por ${VAR:?}, e
// nunca publica portas.
func TestComposeProd_Backup(t *testing.T) {
	bruto, err := os.ReadFile(caminhoComposeProd)
	if err != nil {
		t.Fatal(err)
	}
	b := lerCompose(t, bruto).Services["backup"]
	if b == nil {
		t.Fatal("serviço backup ausente")
	}
	if b["ports"] != nil {
		t.Error("backup não pode publicar portas")
	}
	env := variaveisDeAmbiente(b)
	if env["PGUSER"] != "morfeu_backup" {
		t.Errorf("PGUSER = %q, esperado morfeu_backup", env["PGUSER"])
	}
	for _, k := range []string{"PGPASSWORD", "BACKUP_AGE_RECIPIENT", "BACKUP_DESTINO", "RCLONE_CONFIG_BACKUP_ENDPOINT",
		"RCLONE_CONFIG_BACKUP_ACCESS_KEY_ID", "RCLONE_CONFIG_BACKUP_SECRET_ACCESS_KEY"} {
		if !strings.Contains(env[k], ":?") {
			t.Errorf("%s deveria usar ${VAR:?}: %q", k, env[k])
		}
	}
	if !strings.Contains(env["BACKUP_HEARTBEAT_URL"], ":-") {
		t.Errorf("BACKUP_HEARTBEAT_URL deveria ser opcional (${VAR:-}): %q", env["BACKUP_HEARTBEAT_URL"])
	}
	for k, v := range env {
		if strings.Contains(v, "AGE-SECRET-KEY") {
			t.Errorf("%s carrega chave privada", k)
		}
	}
	dep, _ := b["depends_on"].(map[string]any)
	pg, _ := dep["postgres"].(map[string]any)
	if pg["condition"] != "service_healthy" {
		t.Errorf("backup deveria depender do PG saudável: %v", b["depends_on"])
	}
}

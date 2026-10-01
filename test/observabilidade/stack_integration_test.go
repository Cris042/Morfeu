//go:build integration
// +build integration

// Package observabilidade_test valida a stack de observabilidade versionada
// (PRD 0007): cada componente sobe com a config REAL do repositório em
// container real (ADR 0006) — config inválida ou provisioning quebrado
// falham aqui, não no boot da VM.
package observabilidade_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
	"gopkg.in/yaml.v3"
)

const (
	raiz          = "../.."
	senhaGrafana  = "senha-de-teste-grafana"
	imgPrometheus = "prom/prometheus:v3.15.0"
	imgLoki       = "grafana/loki:3.7.8"
	imgAlloy      = "grafana/alloy:v1.20.1"
	imgGrafana    = "grafana/grafana:13.2.3"
	imgTempo      = "grafana/tempo:2.10.4"
)

func arquivo(t *testing.T, rel, destino string) testcontainers.ContainerFile {
	t.Helper()
	caminho := filepath.Join(raiz, rel)
	if _, err := os.Stat(caminho); err != nil {
		t.Fatalf("arquivo de config ausente: %s", caminho)
	}
	return testcontainers.ContainerFile{HostFilePath: caminho, ContainerFilePath: destino, FileMode: 0o644}
}

// arquivosDe copia recursivamente um diretório de configs para o container.
func arquivosDe(t *testing.T, rel, destino string) []testcontainers.ContainerFile {
	t.Helper()
	var out []testcontainers.ContainerFile
	base := filepath.Join(raiz, rel)
	err := filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		r, _ := filepath.Rel(base, p)
		out = append(out, testcontainers.ContainerFile{HostFilePath: p, ContainerFilePath: destino + "/" + filepath.ToSlash(r), FileMode: 0o644})
		return nil
	})
	if err != nil {
		t.Fatalf("listar %s: %v", base, err)
	}
	return out
}

func iniciar(t *testing.T, req testcontainers.ContainerRequest) testcontainers.Container {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	if c != nil {
		t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	}
	if err != nil {
		t.Fatalf("iniciar %s: %v", req.Image, err)
	}
	return c
}

func endereco(t *testing.T, c testcontainers.Container, porta string) string {
	t.Helper()
	ctx := context.Background()
	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	p, err := c.MappedPort(ctx, porta)
	if err != nil {
		t.Fatalf("porta %s: %v", porta, err)
	}
	return fmt.Sprintf("http://%s:%s", host, p.Port())
}

// TestPrometheus_ConfigValida cobre CA01.
func TestPrometheus_ConfigValida(t *testing.T) {
	t.Parallel() // containers próprios e portas efêmeras: isolados entre si
	c := iniciar(t, testcontainers.ContainerRequest{
		Image:        imgPrometheus,
		ExposedPorts: []string{"9090/tcp"},
		Files:        []testcontainers.ContainerFile{arquivo(t, "configs/prometheus/prometheus.yml", "/etc/prometheus/prometheus.yml")},
		WaitingFor:   wait.ForHTTP("/-/ready").WithPort("9090/tcp").WithStartupTimeout(90 * time.Second),
	})

	code, saida, err := c.Exec(context.Background(), []string{"promtool", "check", "config", "/etc/prometheus/prometheus.yml"})
	if err != nil || code != 0 {
		b, _ := io.ReadAll(saida)
		t.Fatalf("promtool check config falhou (code=%d err=%v): %s", code, err, b)
	}
}

// TestLoki_ConfigValida cobre CA02: Loki fica ready com retenção habilitada.
func TestLoki_ConfigValida(t *testing.T) {
	t.Parallel() // containers próprios e portas efêmeras: isolados entre si
	iniciar(t, testcontainers.ContainerRequest{
		Image:        imgLoki,
		Cmd:          []string{"-config.file=/etc/loki/loki.yml"},
		ExposedPorts: []string{"3100/tcp"},
		Files:        []testcontainers.ContainerFile{arquivo(t, "configs/loki/loki.yml", "/etc/loki/loki.yml")},
		WaitingFor:   wait.ForHTTP("/ready").WithPort("3100/tcp").WithStartupTimeout(120 * time.Second),
	})
}

// TestAlloy_ConfigValida cobre CA04: `alloy validate` falha em sintaxe ou
// componente/argumento inválido (inclui o pipeline de traces — PRD 0040).
func TestAlloy_ConfigValida(t *testing.T) {
	t.Parallel() // containers próprios e portas efêmeras: isolados entre si
	c := iniciar(t, testcontainers.ContainerRequest{
		Image:      imgAlloy,
		Cmd:        []string{"validate", "/etc/alloy/config.alloy"},
		Files:      []testcontainers.ContainerFile{arquivo(t, "configs/alloy/config.alloy", "/etc/alloy/config.alloy")},
		WaitingFor: wait.ForExit().WithExitTimeout(60 * time.Second),
	})
	estado, err := c.State(context.Background())
	if err != nil {
		t.Fatalf("estado do alloy: %v", err)
	}
	if estado.ExitCode != 0 {
		logs, _ := c.Logs(context.Background())
		b, _ := io.ReadAll(logs)
		t.Fatalf("alloy validate saiu com %d: %s", estado.ExitCode, b)
	}
}

func getJSON(t *testing.T, url string, autenticado bool, destino any) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if autenticado {
		req.SetBasicAuth("admin", senhaGrafana)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if destino != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(destino); err != nil {
			t.Fatalf("decodificar %s: %v", url, err)
		}
	}
	return resp.StatusCode
}

// regrasEsperadas é a lista FECHADA de alertas, por uid (task 0041): regra
// nova exige atualizar o PRD e esta lista — não basta mudar uma contagem.
var regrasEsperadas = []string{
	"morfeu-disco-80", "morfeu-api-fora", "morfeu-erro-5xx", "morfeu-dlq-crescendo", "morfeu-consumidor-parado", "morfeu-pg-conexoes",
	"morfeu-refresh-reuso", "morfeu-trava-recusas", "morfeu-sweeper-parado",
	"morfeu-saga-estorno", "morfeu-saga-estorno-preso", "morfeu-saga-reconciliacao-parada", "morfeu-gateway-breaker", "morfeu-saga-latencia",
	"morfeu-email-recusado", "morfeu-email-falhando",
	"morfeu-auditoria-purga-parada", "morfeu-watchdog",
}

// TestGrafana_Provisionamento cobre CA03: o provisioning do repo é aceito no
// boot e expõe 7 dashboards, 3 datasources, as 18 regras (por uid) e os
// contact points do Discord e do heartbeat.
func TestGrafana_Provisionamento(t *testing.T) {
	t.Parallel() // containers próprios e portas efêmeras: isolados entre si
	files := append(arquivosDe(t, "configs/grafana/provisioning", "/etc/grafana/provisioning"),
		arquivosDe(t, "configs/grafana/dashboards", "/var/lib/grafana/dashboards")...)
	c := iniciar(t, testcontainers.ContainerRequest{
		Image:        imgGrafana,
		ExposedPorts: []string{"3000/tcp"},
		Env: map[string]string{
			"GF_SECURITY_ADMIN_PASSWORD": senhaGrafana,
			"GF_AUTH_ANONYMOUS_ENABLED":  "false",
			"GF_USERS_ALLOW_SIGN_UP":     "false",
			"DISCORD_WEBHOOK_URL":        "http://127.0.0.1:9/sem-webhook-configurado",
			"HEALTHCHECKS_PING_URL":      "http://127.0.0.1:9/sem-heartbeat-configurado",
		},
		Files:      files,
		WaitingFor: wait.ForHTTP("/api/health").WithPort("3000/tcp").WithStartupTimeout(120 * time.Second),
	})
	base := endereco(t, c, "3000/tcp")

	// Provisionamento pode terminar logo após o /api/health: todas as
	// contagens usam poll com deadline (robustez a mudanças de ordem no boot).
	var dashboards, regras, contatos []map[string]any
	deadline := time.Now().Add(30 * time.Second)
	for {
		getJSON(t, base+"/api/search?type=dash-db&folderUIDs=morfeu", true, &dashboards)
		getJSON(t, base+"/api/v1/provisioning/alert-rules", true, &regras)
		getJSON(t, base+"/api/v1/provisioning/contact-points", true, &contatos)
		if (len(dashboards) == 7 && len(regras) == len(regrasEsperadas) && len(contatos) > 1) || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Second)
	}
	if len(dashboards) != 7 {
		t.Errorf("esperava 7 dashboards na pasta Morfeu (3 do E0d + 4 de negócio do E10), recebi %d: %v", len(dashboards), dashboards)
	}

	for _, uid := range []string{"prometheus", "loki", "tempo"} {
		if code := getJSON(t, base+"/api/datasources/uid/"+uid, true, nil); code != http.StatusOK {
			t.Errorf("datasource %s: status %d", uid, code)
		}
	}

	provisionadas := map[string]bool{}
	for _, r := range regras {
		if uid, ok := r["uid"].(string); ok {
			provisionadas[uid] = true
		}
	}
	for _, uid := range regrasEsperadas {
		if !provisionadas[uid] {
			t.Errorf("regra %s ausente", uid)
		}
	}
	if len(regras) != len(regrasEsperadas) {
		t.Errorf("esperava exatamente %d regras (lista fechada), recebi %d", len(regrasEsperadas), len(regras))
	}

	tipos := map[string]any{}
	for _, cp := range contatos {
		tipos[fmt.Sprint(cp["name"])] = cp["type"]
	}
	if tipos["discord-morfeu"] != "discord" || tipos["heartbeat-externo"] != "webhook" {
		t.Errorf("contact points esperados (discord-morfeu/discord, heartbeat-externo/webhook): %v", tipos)
	}

	if code := getJSON(t, base+"/api/search", false, nil); code != http.StatusUnauthorized {
		t.Errorf("acesso anônimo deveria ser 401, veio %d", code)
	}
}

// TestCompose_SoGrafanaPublicaPorta cobre CA06 (doc.md §14.5): o compose de
// observabilidade não publica nada além do Grafana em 127.0.0.1.
func TestCompose_SoGrafanaPublicaPorta(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(raiz, "docker-compose.observability.yml"))
	if err != nil {
		t.Fatalf("ler compose: %v", err)
	}
	var compose struct {
		Services map[string]struct {
			Ports []string `yaml:"ports"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(b, &compose); err != nil {
		t.Fatalf("parse do compose: %v", err)
	}
	for nome, svc := range compose.Services {
		for _, p := range svc.Ports {
			if nome != "grafana" || !strings.HasPrefix(p, "127.0.0.1:") {
				t.Errorf("serviço %s publica porta proibida: %q", nome, p)
			}
		}
	}
}

// spanOTLP monta um trace OTLP/JSON de 1 span (ids em hex, como o OTLP/HTTP).
func spanOTLP(traceID string, erro bool, duracao time.Duration) string {
	fim := time.Now()
	inicio := fim.Add(-duracao)
	status := 1
	if erro {
		status = 2
	}
	return fmt.Sprintf(`{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"morfeu-teste"}}]},`+
		`"scopeSpans":[{"spans":[{"traceId":"%s","spanId":"%s","name":"GET /teste","kind":2,`+
		`"startTimeUnixNano":"%d","endTimeUnixNano":"%d","status":{"code":%d}}]}]}]}`,
		traceID, traceID[:16], inicio.UnixNano(), fim.UnixNano(), status)
}

// TestTraces_TailSamplingAteOTempo (PRD 0040, ADR 0012): Alloy e Tempo com
// as configs reais numa rede própria; um trace com erro e um lento enviados
// ao receptor OTLP do Alloy chegam ao Tempo (100% dos erros e dos lentos).
// O rápido sem erro é probabilístico (10%) — não se afirma nada sobre ele.
func TestTraces_TailSamplingAteOTempo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rede, err := network.New(ctx)
	if err != nil {
		t.Fatalf("rede: %v", err)
	}
	t.Cleanup(func() { _ = rede.Remove(context.Background()) })
	tempo := iniciar(t, testcontainers.ContainerRequest{
		Image:          imgTempo,
		Cmd:            []string{"-config.file=/etc/tempo/tempo.yml"},
		ExposedPorts:   []string{"3200/tcp"},
		Networks:       []string{rede.Name},
		NetworkAliases: map[string][]string{rede.Name: {"tempo"}},
		Files:          []testcontainers.ContainerFile{arquivo(t, "configs/tempo/tempo.yml", "/etc/tempo/tempo.yml")},
		WaitingFor:     wait.ForHTTP("/ready").WithPort("3200/tcp").WithStartupTimeout(120 * time.Second),
	})
	alloy := iniciar(t, testcontainers.ContainerRequest{
		Image:        imgAlloy,
		Cmd:          []string{"run", "--server.http.listen-addr=0.0.0.0:12345", "/etc/alloy/config.alloy"},
		ExposedPorts: []string{"4318/tcp", "12345/tcp"},
		Networks:     []string{rede.Name},
		Files:        []testcontainers.ContainerFile{arquivo(t, "configs/alloy/config.alloy", "/etc/alloy/config.alloy")},
		WaitingFor:   wait.ForListeningPort("4318/tcp").WithStartupTimeout(90 * time.Second),
	})
	otlp := endereco(t, alloy, "4318/tcp") + "/v1/traces"
	ids := map[string]string{
		"erro":  "0af7651916cd43dd8448eb211c80319c",
		"lento": "1bf7651916cd43dd8448eb211c80319d",
	}
	enviar := func(corpo string) {
		resp, err := http.Post(otlp, "application/json", strings.NewReader(corpo))
		if err != nil {
			t.Fatalf("POST OTLP: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("OTLP respondeu %d", resp.StatusCode)
		}
	}
	enviar(spanOTLP(ids["erro"], true, 5*time.Millisecond))
	enviar(spanOTLP(ids["lento"], false, 900*time.Millisecond))

	base := endereco(t, tempo, "3200/tcp")
	for nome, id := range ids {
		deadline := time.Now().Add(60 * time.Second) // decision_wait 10 s + ingestão
		for {
			resp, err := http.Get(base + "/api/traces/" + id)
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					break
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("trace %s (%s) não chegou ao Tempo pelo tail sampling", nome, id)
			}
			time.Sleep(2 * time.Second)
		}
	}
}

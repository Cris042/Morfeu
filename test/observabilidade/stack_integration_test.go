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
	iniciar(t, testcontainers.ContainerRequest{
		Image:        imgLoki,
		Cmd:          []string{"-config.file=/etc/loki/loki.yml"},
		ExposedPorts: []string{"3100/tcp"},
		Files:        []testcontainers.ContainerFile{arquivo(t, "configs/loki/loki.yml", "/etc/loki/loki.yml")},
		WaitingFor:   wait.ForHTTP("/ready").WithPort("3100/tcp").WithStartupTimeout(120 * time.Second),
	})
}

// TestAlloy_ConfigValida cobre CA04: `alloy fmt` falha em sintaxe inválida.
func TestAlloy_ConfigValida(t *testing.T) {
	c := iniciar(t, testcontainers.ContainerRequest{
		Image:      imgAlloy,
		Cmd:        []string{"fmt", "/etc/alloy/config.alloy"},
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
		t.Fatalf("alloy fmt saiu com %d: %s", estado.ExitCode, b)
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

// TestGrafana_Provisionamento cobre CA03: o provisioning do repo é aceito no
// boot e expõe 3 dashboards, 2 datasources, 6 alertas e o contact point.
func TestGrafana_Provisionamento(t *testing.T) {
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
		},
		Files:      files,
		WaitingFor: wait.ForHTTP("/api/health").WithPort("3000/tcp").WithStartupTimeout(120 * time.Second),
	})
	base := endereco(t, c, "3000/tcp")

	var dashboards []map[string]any
	deadline := time.Now().Add(30 * time.Second)
	for {
		getJSON(t, base+"/api/search?type=dash-db&folderUIDs=morfeu", true, &dashboards)
		if len(dashboards) == 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Second)
	}
	if len(dashboards) != 3 {
		t.Errorf("esperava 3 dashboards na pasta Morfeu, recebi %d: %v", len(dashboards), dashboards)
	}

	for _, uid := range []string{"prometheus", "loki"} {
		if code := getJSON(t, base+"/api/datasources/uid/"+uid, true, nil); code != http.StatusOK {
			t.Errorf("datasource %s: status %d", uid, code)
		}
	}

	var regras []map[string]any
	getJSON(t, base+"/api/v1/provisioning/alert-rules", true, &regras)
	if len(regras) != 6 {
		t.Errorf("esperava 6 regras de alerta (lista fechada), recebi %d", len(regras))
	}

	var contatos []map[string]any
	getJSON(t, base+"/api/v1/provisioning/contact-points", true, &contatos)
	achou := false
	for _, cp := range contatos {
		if cp["name"] == "discord-morfeu" && cp["type"] == "discord" {
			achou = true
		}
	}
	if !achou {
		t.Errorf("contact point discord-morfeu ausente: %v", contatos)
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

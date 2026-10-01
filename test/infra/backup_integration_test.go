//go:build integration
// +build integration

package infra

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Task 0043 (ADR 0013): ida e volta do backup com a imagem REAL
// (deploy/backup/Dockerfile) — PG com roles + migrations + dados sintéticos →
// backup.sh (backend `local` do rclone) → restore.sh num PG efêmero →
// invariantes. MinIO (S3 de verdade) só com BACKUP_TESTE_S3=1.

const (
	imagemBackupTeste = "morfeu-backup-teste:t"
	senhaRestore      = "senha-restore-so-de-teste"
	emailSeed         = "cliente.sintetico@exemplo.invalid"
	portaHB           = "8099"
	caminhoHB         = "/hb-sucesso-so-de-teste"
	dirDestino        = "/home/backup/destino"
	chaveCerta        = "/home/backup/chave.key"
	chaveErrada       = "/home/backup/outra.key"
)

var (
	imagemOnce sync.Once
	imagemErr  error
)

// garantirImagemBackup builda a imagem uma vez por execução. O contexto é
// montado num diretório temporário só com scripts/backup + o Dockerfile — o
// mesmo conteúdo do contexto do compose (./scripts/backup), sem arrastar a
// raiz do repositório (web/node_modules etc.).
func garantirImagemBackup(t *testing.T) {
	t.Helper()
	imagemOnce.Do(func() { imagemErr = construirImagemBackup() })
	if imagemErr != nil {
		t.Fatalf("build da imagem de backup: %v", imagemErr)
	}
}

func construirImagemBackup() error {
	dir, err := os.MkdirTemp("", "ctx-backup-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	origens := map[string]string{"Dockerfile": "../../deploy/backup/Dockerfile"}
	scripts, err := filepath.Glob("../../scripts/backup/*")
	if err != nil {
		return err
	}
	for _, s := range scripts {
		origens[filepath.Base(s)] = s
	}
	for destino, origem := range origens {
		bruto, err := os.ReadFile(origem) //nolint:gosec // G304: arquivos do próprio repositório
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, destino), bruto, 0o600); err != nil { //nolint:gosec // G703: nomes fixos do repositório, dir é temporário
			return err
		}
	}
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			FromDockerfile: testcontainers.FromDockerfile{
				Context: dir, Dockerfile: "Dockerfile", Repo: "morfeu-backup-teste", Tag: "t", KeepImage: true,
			},
		},
		Started: false,
	})
	if err != nil {
		return err
	}
	return c.Terminate(ctx)
}

// pingSh é o "healthchecks.io" do teste: um servidor HTTP mínimo (busybox nc)
// dentro do container de ferramentas que registra a linha de requisição.
const pingSh = `#!/bin/sh
read -r linha
echo "$linha" >> /home/backup/pings
while read -r h && [ "$h" != "$(printf '\r')" ]; do :; done
printf 'HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n'
`

type ambienteBackup struct {
	rede         string
	origemHost   string // PG de origem, mapeado no host do teste
	origemPorta  string
	restoreHost  string
	restorePorta string
	f            testcontainers.Container
}

// exec roda `env K=V... cmd` como o usuário backup do container de ferramentas.
func (a *ambienteBackup) exec(t *testing.T, env map[string]string, cmd ...string) (int, string) {
	t.Helper()
	chaves := make([]string, 0, len(env))
	for k := range env {
		chaves = append(chaves, k)
	}
	sort.Strings(chaves)
	args := []string{"env"}
	for _, k := range chaves {
		args = append(args, k+"="+env[k])
	}
	args = append(args, cmd...)
	code, r, err := a.f.Exec(context.Background(), args, tcexec.Multiplexed())
	if err != nil {
		t.Fatalf("exec %v: %v", cmd, err)
	}
	saida, _ := io.ReadAll(r)
	return code, string(saida)
}

func (a *ambienteBackup) sh(t *testing.T, script string) string {
	t.Helper()
	code, out := a.exec(t, nil, "sh", "-c", script)
	if code != 0 {
		t.Fatalf("sh %q: código %d: %s", script, code, out)
	}
	return strings.TrimSpace(out)
}

func (a *ambienteBackup) pings(t *testing.T) []string {
	t.Helper()
	bruto := a.sh(t, "cat /home/backup/pings 2>/dev/null || true")
	if bruto == "" {
		return nil
	}
	return strings.Split(bruto, "\n")
}

func (a *ambienteBackup) limparPings(t *testing.T) { t.Helper(); a.sh(t, ": > /home/backup/pings") }

func (a *ambienteBackup) objetos(t *testing.T, dir string) []string {
	t.Helper()
	bruto := a.sh(t, "find "+dir+" -type f 2>/dev/null | sort || true")
	if bruto == "" {
		return nil
	}
	return strings.Split(bruto, "\n")
}

func (a *ambienteBackup) ler(t *testing.T, caminho string) []byte {
	t.Helper()
	r, err := a.f.CopyFileFromContainer(context.Background(), caminho)
	if err != nil {
		t.Fatalf("copiar %s: %v", caminho, err)
	}
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// envBackup é o ambiente do job de backup; sobrescritas vão por cima.
func (a *ambienteBackup) envBackup(recipient string, extra map[string]string) map[string]string {
	e := map[string]string{
		"PGHOST": "origem", "PGPORT": "5432", "PGUSER": "morfeu_backup", "PGPASSWORD": senhaBackup, "PGDATABASE": bancoRoles,
		"BACKUP_AGE_RECIPIENT": recipient, "BACKUP_DESTINO": "backup:" + dirDestino,
		"RCLONE_CONFIG_BACKUP_TYPE": "local",
		"BACKUP_HEARTBEAT_URL":      "http://127.0.0.1:" + portaHB + caminhoHB,
	}
	for k, v := range extra {
		e[k] = v
	}
	return e
}

func (a *ambienteBackup) envRestore(identidade string, extra map[string]string) map[string]string {
	e := map[string]string{
		"BACKUP_DESTINO": "backup:" + dirDestino, "RCLONE_CONFIG_BACKUP_TYPE": "local", "BACKUP_AGE_IDENTITY": identidade,
		"RESTORE_PGHOST": "restore", "RESTORE_PGUSER": "postgres", "RESTORE_PGPASSWORD": senhaRestore, "RESTORE_PGDATABASE": bancoRoles,
	}
	for k, v := range extra {
		e[k] = v
	}
	return e
}

func subirPGRestore(t *testing.T, rede string) (host, porta string) {
	t.Helper()
	ctx := context.Background()
	pg, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          "postgres:16-alpine",
			ExposedPorts:   []string{"5432/tcp"},
			Env:            map[string]string{"POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": senhaRestore, "POSTGRES_DB": bancoRoles},
			Networks:       []string{rede},
			NetworkAliases: map[string][]string{rede: {"restore"}},
			WaitingFor:     wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(120 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("subir PG de restore: %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(context.Background()) })
	host, _ = pg.Host(ctx)
	p, err := pg.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return host, p.Port()
}

// montarAmbienteBackup sobe rede, PG de origem (roles + migrations + seed),
// PG de restore vazio e o container de ferramentas (imagem real), já com as
// duas chaves age e o servidor de heartbeat.
func montarAmbienteBackup(t *testing.T) *ambienteBackup {
	t.Helper()
	ctx := context.Background()
	garantirImagemBackup(t)

	rede, err := network.New(ctx)
	if err != nil {
		t.Fatalf("rede: %v", err)
	}
	t.Cleanup(func() { _ = rede.Remove(context.Background()) })

	a := &ambienteBackup{rede: rede.Name}
	a.origemHost, a.origemPorta = subirPGComRolesEm(t, rede.Name, "origem")
	semearOrigem(t, a.origemHost, a.origemPorta)
	a.restoreHost, a.restorePorta = subirPGRestore(t, rede.Name)

	f, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:    imagemBackupTeste,
			Networks: []string{rede.Name},
			// nc faz o papel do healthchecks.io; o container fica vivo para os exec.
			Entrypoint: []string{"sh", "-c", "nc -lk -p " + portaHB + " -e /home/backup/ping.sh >/dev/null 2>&1 & exec sleep 3600"},
			Files: []testcontainers.ContainerFile{{
				Reader: strings.NewReader(pingSh), ContainerFilePath: "/home/backup/ping.sh", FileMode: 0o755,
			}},
			WaitingFor: wait.ForExec([]string{"sh", "-c", "nc -z 127.0.0.1 " + portaHB}).WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("subir ferramentas de backup: %v", err)
	}
	t.Cleanup(func() { _ = f.Terminate(context.Background(), testcontainers.StopTimeout(0)) })
	a.f = f

	a.sh(t, "age-keygen -o "+chaveCerta+" 2>/dev/null && age-keygen -o "+chaveErrada+" 2>/dev/null")
	return a
}

func (a *ambienteBackup) recipient(t *testing.T, chave string) string {
	t.Helper()
	return a.sh(t, "age-keygen -y "+chave)
}

// semearOrigem aplica as migrations como morfeu_migrator e grava dados sintéticos
// (incluindo um e-mail conhecido e linhas na trilha e nas travas).
func semearOrigem(t *testing.T, host, porta string) {
	t.Helper()
	ctx := context.Background()
	m, err := migrate.New("file://../../migrations", urlDoRole(host, porta, "morfeu_migrator", senhaMigr))
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	_, _ = m.Close()

	super := abrirPool(t, urlDoRole(host, porta, "postgres", "postgres-so-de-teste"))
	stmts := []string{
		`INSERT INTO usuario (id, nome, email, senha_hash, papel) VALUES (gen_random_uuid(), 'Cliente Sintético', '` + emailSeed + `', 'hash-sintetico', 'cliente')`,
		`INSERT INTO salas (nome, layout) VALUES ('Sala backup', '{}')`,
		`INSERT INTO sessoes (filme_id, sala_id, inicio, duracao_min, fim, preco_centavos)
		   SELECT 1, id, now() + interval '1 day', 120, now() + interval '1 day 140 minutes', 2500 FROM salas WHERE nome = 'Sala backup'`,
		`INSERT INTO holds (id, sessao_id, assento_codigo, dono_hash, status, expires_at, criado_em, atualizado_em)
		   SELECT gen_random_uuid(), id, 'A1', decode(repeat('ab', 32), 'hex'), 'ativo', now() + interval '10 minutes', now(), now() FROM sessoes`,
		`INSERT INTO pedidos (id, codigo, email, dono_hash, sessao_id, assentos, total_centavos, status, expira_em, criado_em, atualizado_em)
		   SELECT '11111111-1111-4111-8111-111111111111', 'ABCDEFGHIJKLMNOP', '` + emailSeed + `', decode(repeat('cd', 32), 'hex'), id, '{B2}', 2500, 'pago', now(), now(), now() FROM sessoes`,
		`INSERT INTO pedido_eventos (pedido_id, de, para, ocorrido_em) VALUES ('11111111-1111-4111-8111-111111111111', 'aguardando_pagamento', 'pago', now())`,
		`INSERT INTO eventos_auditoria (ator_id, acao, alvo_tipo, alvo_id, ocorrido_em) VALUES (gen_random_uuid(), 'filme_criado', 'filme', '1', now())`,
	}
	for _, s := range stmts {
		if _, err := super.Exec(ctx, s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
}

// objetoCom devolve o primeiro objeto com o sufixo dado (ou "").
func objetoCom(objs []string, sufixo string) string {
	for _, o := range objs {
		if strings.HasSuffix(o, sufixo) {
			return o
		}
	}
	return ""
}

// TestBackupIdaEVolta cobre CA01–CA05 com a imagem real e o backend local do rclone.
func TestBackupIdaEVolta(t *testing.T) { //nolint:gocognit,funlen // cenário único com subtestes sequenciais que compartilham o ambiente
	a := montarAmbienteBackup(t)
	recipient := a.recipient(t, chaveCerta)
	var relativo string // diario/… ou semanal/… do backup bem-sucedido

	t.Run("CA01 ida e volta com invariantes", func(t *testing.T) {
		inicio := time.Now()
		code, out := a.exec(t, a.envBackup(recipient, nil), "bash", "/opt/backup/backup.sh")
		if code != 0 {
			t.Fatalf("backup falhou (%d): %s", code, out)
		}
		t.Logf("backup: %s (%s)", strings.TrimSpace(out), time.Since(inicio).Round(time.Millisecond))
		for _, proibido := range []string{caminhoHB, senhaBackup, recipient} {
			if strings.Contains(out, proibido) {
				t.Errorf("a saída do backup vaza %q", proibido)
			}
		}

		objs := a.objetos(t, dirDestino)
		dump, glob := objetoCom(objs, ".dump.age"), objetoCom(objs, ".globals.age")
		sha, man := objetoCom(objs, ".sha256"), objetoCom(objs, ".manifesto")
		if dump == "" || glob == "" || sha == "" || man == "" || len(objs) != 4 {
			t.Fatalf("objetos esperados (dump, globals, sha256, manifesto), vieram: %v", objs)
		}
		relativo = strings.TrimPrefix(dump, dirDestino+"/")
		esperadoPrefixo := "diario/"
		if time.Now().UTC().Weekday() == time.Sunday {
			esperadoPrefixo = "semanal/"
		}
		if !strings.HasPrefix(relativo, esperadoPrefixo+"morfeu-") {
			t.Errorf("prefixo = %q, esperado %s", relativo, esperadoPrefixo)
		}

		// Cifrado: formato age e nenhum valor conhecido do seed em claro.
		for _, o := range []string{dump, glob} {
			b := a.ler(t, o)
			if !bytes.HasPrefix(b, []byte("age-encryption.org/v1")) {
				t.Errorf("%s não é um arquivo age", o)
			}
			if bytes.Contains(b, []byte(emailSeed)) || bytes.Contains(b, []byte("PGDMP")) {
				t.Errorf("%s contém dado em claro", o)
			}
		}
		manifesto := string(a.ler(t, man))
		if strings.Contains(manifesto, emailSeed) || !strings.Contains(manifesto, "tabela\tusuario\t1\n") ||
			!strings.Contains(manifesto, "versao\tschema_migrations\t") {
			t.Errorf("manifesto inesperado:\n%s", manifesto)
		}
		// Heartbeat: sucesso → GET na URL; nunca /fail.
		p := a.pings(t)
		if len(p) != 1 || strings.TrimSpace(p[0]) != "GET "+caminhoHB+" HTTP/1.1" {
			t.Errorf("pings após sucesso = %q", p)
		}

		// Restore em PG efêmero + invariantes.
		inicio = time.Now()
		code, out = a.exec(t, a.envRestore(chaveCerta, nil), "bash", "/opt/backup/restore.sh", relativo)
		if code != 0 {
			t.Fatalf("restore falhou (%d): %s", code, out)
		}
		t.Logf("restore (RTO medido pelo script e pelo teste = %s):\n%s", time.Since(inicio).Round(time.Millisecond), out)
		if !strings.Contains(out, "invariantes verificadas") || !strings.Contains(out, "RTO") {
			t.Errorf("saída do restore sem invariantes/RTO: %s", out)
		}
		restore := abrirPool(t, urlDoRole(a.restoreHost, a.restorePorta, "postgres", senhaRestore))
		var n int
		if err := restore.QueryRow(context.Background(), `SELECT count(*) FROM usuario WHERE email = $1`, emailSeed).Scan(&n); err != nil || n != 1 {
			t.Errorf("usuário do seed no restore: n=%d err=%v", n, err)
		}
	})
	if relativo == "" {
		t.Fatal("sem backup válido; abortando os demais cenários")
	}

	t.Run("invariantes reprovam divergência com o manifesto", func(t *testing.T) {
		restore := abrirPool(t, urlDoRole(a.restoreHost, a.restorePorta, "postgres", senhaRestore))
		// O restore anterior está intacto; apagar uma linha (como um dump truncado) tem de ser pego.
		if _, err := restore.Exec(context.Background(), `DELETE FROM pedido_eventos`); err != nil {
			t.Fatal(err)
		}
		man := strings.TrimSuffix(relativo, ".dump.age") + ".manifesto"
		code, out := a.exec(t, nil, "sh", "-c",
			"{ echo 'CREATE TEMP TABLE manifesto (tipo text, nome text, valor text);'; echo 'COPY manifesto FROM STDIN;'; cat "+dirDestino+"/"+man+
				"; echo '\\.'; cat /opt/backup/invariantes.sql; } | PGHOST=restore PGUSER=postgres PGPASSWORD="+senhaRestore+" PGDATABASE="+bancoRoles+" psql -qX -v ON_ERROR_STOP=1")
		if code == 0 || !strings.Contains(out, "pedido_eventos") {
			t.Errorf("esperado reprovação citando pedido_eventos; código %d: %s", code, out)
		}
	})

	t.Run("CA02 chave errada falha com mensagem clara", func(t *testing.T) {
		code, out := a.exec(t, a.envRestore(chaveErrada, nil), "bash", "/opt/backup/restore.sh", relativo)
		if code == 0 || !strings.Contains(out, "chave errada") {
			t.Errorf("esperado falha com 'chave errada'; código %d: %s", code, out)
		}
	})

	t.Run("CA03 objeto corrompido falha no sha256", func(t *testing.T) {
		a.sh(t, "rm -rf "+dirDestino+"-ruim && cp -r "+dirDestino+" "+dirDestino+"-ruim && "+
			"printf 'X' | dd of="+dirDestino+"-ruim/"+relativo+" bs=1 seek=100 conv=notrunc 2>/dev/null")
		code, out := a.exec(t, a.envRestore(chaveCerta, map[string]string{"BACKUP_DESTINO": "backup:" + dirDestino + "-ruim"}),
			"bash", "/opt/backup/restore.sh", relativo)
		if code == 0 || !strings.Contains(out, "sha256") {
			t.Errorf("esperado falha de sha256; código %d: %s", code, out)
		}
	})

	t.Run("CA04 destino indisponível: saída ≠ 0 e ping de falha", func(t *testing.T) {
		a.limparPings(t)
		code, out := a.exec(t, a.envBackup(recipient, map[string]string{"BACKUP_DESTINO": "backup:/proc/sem-permissao"}), "bash", "/opt/backup/backup.sh")
		if code == 0 {
			t.Fatalf("backup deveria falhar: %s", out)
		}
		if strings.Contains(out, caminhoHB) {
			t.Errorf("a saída vaza a URL do heartbeat: %s", out)
		}
		p := a.pings(t)
		if len(p) != 1 || !strings.HasPrefix(p[0], "GET "+caminhoHB+"/fail ") {
			t.Errorf("esperado só o ping de falha, veio %q", p)
		}
	})

	t.Run("agendador sobrevive à falha e loga uma linha por execução", func(t *testing.T) {
		code, out := a.exec(t, a.envBackup(recipient, map[string]string{
			"BACKUP_DESTINO": "backup:/proc/sem-permissao", "BACKUP_HEARTBEAT_URL": "",
			"BACKUP_AGENDAR_SEM_ESPERA": "1", "BACKUP_AGENDAR_MAX_EXECUCOES": "2",
		}), "bash", "/opt/backup/agendar.sh")
		if code != 0 || strings.Count(out, "fim resultado=falha") != 2 {
			t.Errorf("código %d; esperado 2 execuções falhas e saída 0: %s", code, out)
		}
	})

	t.Run("CA05 banco sem schema_migrations é recusado", func(t *testing.T) {
		_, _ = subirPGComRolesEm(t, a.rede, "vazio") // roles sem migrations
		a.limparPings(t)
		code, out := a.exec(t, a.envBackup(recipient, map[string]string{
			"PGHOST": "vazio", "BACKUP_DESTINO": "backup:" + dirDestino + "-vazio",
		}), "bash", "/opt/backup/backup.sh")
		if code == 0 || !strings.Contains(out, "schema_migrations") {
			t.Errorf("esperado recusa citando schema_migrations; código %d: %s", code, out)
		}
		if n := len(a.objetos(t, dirDestino+"-vazio")); n != 0 {
			t.Errorf("nenhum objeto deveria ser gravado, vieram %d", n)
		}
		if p := a.pings(t); len(p) != 1 || !strings.Contains(p[0], "/fail") {
			t.Errorf("esperado ping de falha, veio %q", p)
		}
	})
}

// TestBackupS3 repete o ciclo contra um servidor S3 de verdade (protocolo S3,
// multipart, credenciais) — o `rclone serve s3` da própria imagem, porque as
// imagens públicas do MinIO deixaram de ser publicadas. Fora do PR: só com
// BACKUP_TESTE_S3=1.
func TestBackupS3(t *testing.T) {
	if os.Getenv("BACKUP_TESTE_S3") != "1" {
		t.Skip("defina BACKUP_TESTE_S3=1 para rodar o ciclo contra um servidor S3")
	}
	a := montarAmbienteBackup(t)
	const (
		usuarioS3 = "s3-so-de-teste"
		senhaS3   = "senha-s3-so-de-teste-123"
		bucket    = "morfeu-backups"
	)
	a.sh(t, "mkdir -p /home/backup/s3data/"+bucket+" && (rclone serve s3 --auth-key "+usuarioS3+","+senhaS3+" --addr 127.0.0.1:9000 /home/backup/s3data >/dev/null 2>&1 &) ; "+
		"for i in 1 2 3 4 5 6 7 8 9 10; do nc -z 127.0.0.1 9000 && exit 0; sleep 1; done; exit 1")

	s3 := func(remote string) map[string]string {
		p := "RCLONE_CONFIG_" + strings.ToUpper(remote) + "_"
		return map[string]string{
			p + "TYPE": "s3", p + "PROVIDER": "Other", p + "ENDPOINT": "http://127.0.0.1:9000", p + "REGION": "us-east-1",
			p + "ACCESS_KEY_ID": usuarioS3, p + "SECRET_ACCESS_KEY": senhaS3, p + "FORCE_PATH_STYLE": "true", p + "NO_CHECK_BUCKET": "true",
		}
	}
	recipient := a.recipient(t, chaveCerta)
	envB := a.envBackup(recipient, s3("backup"))
	envB["RCLONE_CONFIG_BACKUP_TYPE"] = "s3"
	envB["BACKUP_DESTINO"] = "backup:" + bucket
	if code, out := a.exec(t, envB, "bash", "/opt/backup/backup.sh"); code != 0 {
		t.Fatalf("backup S3: %s", out)
	}
	_, lista := a.exec(t, s3("admin"), "rclone", "lsf", "-R", "admin:"+bucket)
	var dump string
	for _, l := range strings.Fields(lista) {
		if strings.HasSuffix(l, ".dump.age") {
			dump = l
		}
	}
	if dump == "" {
		t.Fatalf("dump não encontrado no bucket: %s", lista)
	}
	envR := a.envRestore(chaveCerta, s3("backup"))
	envR["RCLONE_CONFIG_BACKUP_TYPE"] = "s3"
	envR["BACKUP_DESTINO"] = "backup:" + bucket
	if code, out := a.exec(t, envR, "bash", "/opt/backup/restore.sh", dump); code != 0 {
		t.Fatalf("restore S3: %s", out)
	}
}

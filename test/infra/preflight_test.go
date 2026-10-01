package infra

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// chavePublicaAge tem o formato de um destinatário age (pública por definição).
const chavePublicaAge = "age1" + "qyqszqgpqyqszqgpqyqszqgpqyqszqgpqyqszqgpqyqszqgpqyqsz8lsn7"

const senhaForte = "a3f9c1d27b8e4f60a1b2c3d4e5f60718" // 32 caracteres, sem placeholder

// chavePrivadaAge é montada em runtime: o literal contíguo dispararia o gitleaks (falso positivo).
var chavePrivadaAge = "AGE-SECRET-" + "KEY-" + strings.Repeat("1", 40)

func envProd(sobrescritas map[string]string) string {
	v := map[string]string{
		"POSTGRES_USER": "admin_cinema", "POSTGRES_PASSWORD": senhaForte,
		"PG_MIGRATOR_PASSWORD": senhaForte, "PG_APP_PASSWORD": senhaForte,
		"PG_PURGE_PASSWORD": senhaForte, "PG_BACKUP_PASSWORD": senhaForte,
		"PG_MONITOR_PASSWORD": senhaForte, "REDIS_PASSWORD": senhaForte,
		"RABBITMQ_DEFAULT_USER": "mq_cinema", "RABBITMQ_DEFAULT_PASS": senhaForte,
		"BACKUP_AGE_RECIPIENT": chavePublicaAge, "BACKUP_S3_ENDPOINT": "https://ns.compat.objectstorage.sa-saopaulo-1.oraclecloud.com",
		"BACKUP_S3_REGION": "sa-saopaulo-1", "BACKUP_S3_BUCKET": "cinema-backups",
		"BACKUP_S3_ACCESS_KEY_ID": "chaveid" + senhaForte, "BACKUP_S3_SECRET_ACCESS_KEY": "segredo" + senhaForte,
		"BACKUP_HEARTBEAT_URL": "https://hc-ping.com/0a1b2c3d",
	}
	for k, val := range sobrescritas {
		v[k] = val
	}
	var b strings.Builder
	b.WriteString("# comentário\n")
	for k, val := range v {
		b.WriteString(k + "=" + val + "\n")
	}
	return b.String()
}

// rodarPreflight grava o conteúdo num arquivo temporário e roda o script real.
func rodarPreflight(t *testing.T, nome, conteudo string) (saida string, ok bool) {
	t.Helper()
	arquivo := filepath.Join(t.TempDir(), nome)
	if err := os.WriteFile(arquivo, []byte(conteudo), 0o600); err != nil { //nolint:gosec // G703: arquivo em t.TempDir()
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "../../scripts/preflight.sh", arquivo) //nolint:gosec // G204: script do repo, arquivo em t.TempDir()
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// TestPreflight cobre CA02: válido passa; vazio, ausente, placeholder e senha
// curta falham — e nenhuma saída ecoa valores.
func TestPreflight(t *testing.T) {
	casos := []struct {
		nome, arquivo, conteudo string
		passa                   bool
		variavel                string
	}{
		{"prod válido", ".env.prod", envProd(nil), true, ""},
		{"senha vazia", ".env.prod", envProd(map[string]string{"REDIS_PASSWORD": ""}), false, "REDIS_PASSWORD"},
		{"placeholder troque", ".env.prod", envProd(map[string]string{"PG_APP_PASSWORD": "troque-esta-senha-por-algo-bem-longo"}), false, "PG_APP_PASSWORD"}, //nolint:gosec // G101: valor fictício de teste
		{"placeholder postgres", ".env.prod", envProd(map[string]string{"POSTGRES_PASSWORD": "postgres" + senhaForte}), false, "POSTGRES_PASSWORD"},
		{"senha curta", ".env.prod", envProd(map[string]string{"PG_PURGE_PASSWORD": strings.Repeat("x", 12)}), false, "PG_PURGE_PASSWORD"},
		{"usuário padrão", ".env.prod", envProd(map[string]string{"RABBITMQ_DEFAULT_USER": "guest"}), false, "RABBITMQ_DEFAULT_USER"},
		{"variável ausente", ".env.prod", strings.ReplaceAll(envProd(nil), "PG_BACKUP_PASSWORD="+senhaForte+"\n", ""), false, "PG_BACKUP_PASSWORD"},
		{"backup: recipient ausente", ".env.prod", strings.ReplaceAll(envProd(nil), "BACKUP_AGE_RECIPIENT="+chavePublicaAge+"\n", ""), false, "BACKUP_AGE_RECIPIENT"},
		{"backup: recipient placeholder", ".env.prod", envProd(map[string]string{"BACKUP_AGE_RECIPIENT": "age1troque-pela-chave-publica"}), false, "BACKUP_AGE_RECIPIENT"},
		{"backup: recipient não é age1", ".env.prod", envProd(map[string]string{"BACKUP_AGE_RECIPIENT": "ssh-ed25519 AAAA"}), false, "BACKUP_AGE_RECIPIENT"},
		{"backup: chave privada no .env", ".env.prod", envProd(nil) + "# " + chavePrivadaAge + "\n", false, "AGE-SECRET-KEY"},
		{"backup: chave privada no lugar do recipient", ".env.prod", envProd(map[string]string{"BACKUP_AGE_RECIPIENT": chavePrivadaAge}), false, "BACKUP_AGE_RECIPIENT"},
		{"backup: credencial S3 vazia", ".env.prod", envProd(map[string]string{"BACKUP_S3_SECRET_ACCESS_KEY": ""}), false, "BACKUP_S3_SECRET_ACCESS_KEY"},
		{"backup: endpoint http", ".env.prod", envProd(map[string]string{"BACKUP_S3_ENDPOINT": "http://s3.exemplo.com"}), false, "BACKUP_S3_ENDPOINT"},
		{"backup: heartbeat http", ".env.prod", envProd(map[string]string{"BACKUP_HEARTBEAT_URL": "http://hc-ping.com/0a1b2c3d"}), false, "BACKUP_HEARTBEAT_URL"},
		{"backup: heartbeat vazio é opcional", ".env.prod", envProd(map[string]string{"BACKUP_HEARTBEAT_URL": ""}), true, ""},
		{"observability válido", ".env.observability", "GF_SECURITY_ADMIN_PASSWORD=" + senhaForte + "\nDATA_SOURCE_PASS=" + senhaForte +
			"\nDISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/1/abc\nHEALTHCHECKS_PING_URL=https://hc-ping.com/0a1b2c3d\n", true, ""},
		{"observability com placeholder de dev", ".env.observability", "GF_SECURITY_ADMIN_PASSWORD=" + senhaForte + "\nDATA_SOURCE_PASS=" + senhaForte +
			"\nDISCORD_WEBHOOK_URL=http://127.0.0.1:9/sem-webhook-configurado\nHEALTHCHECKS_PING_URL=https://hc-ping.com/0a1b2c3d\n", false, "DISCORD_WEBHOOK_URL"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			saida, ok := rodarPreflight(t, c.arquivo, c.conteudo)
			if ok != c.passa {
				t.Fatalf("passa=%t, esperado %t; saída: %s", ok, c.passa, saida)
			}
			if c.variavel != "" && !strings.Contains(saida, c.variavel) {
				t.Errorf("a saída deveria citar %s: %s", c.variavel, saida)
			}
			for _, segredo := range []string{senhaForte, "troque-esta-senha-por-algo", "abc123XYZ789", "sem-webhook", chavePrivadaAge, "segredo" + senhaForte} {
				if strings.Contains(saida, segredo) {
					t.Errorf("a saída ecoa um valor (%q): %s", segredo, saida)
				}
			}
		})
	}
}

// TestPreflight_ExemploVersionadoEhRejeitado: o .env.prod.example só tem
// placeholders — copiá-lo sem editar nunca pode passar.
func TestPreflight_ExemploVersionadoEhRejeitado(t *testing.T) {
	bruto, err := os.ReadFile("../../.env.prod.example")
	if err != nil {
		t.Fatal(err)
	}
	if saida, ok := rodarPreflight(t, ".env.prod", string(bruto)); ok {
		t.Fatalf("o exemplo não deveria passar no preflight: %s", saida)
	}
}

func TestPreflight_ArquivoAusente(t *testing.T) {
	out, err := exec.Command("bash", "../../scripts/preflight.sh", filepath.Join(t.TempDir(), "nao-existe.env")).CombinedOutput() //nolint:gosec // G204: script do repo
	if err == nil {
		t.Fatalf("arquivo ausente deveria falhar: %s", out)
	}
}

package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// Clear environment
	os.Clearenv()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.AppPort != "8080" {
		t.Errorf("Expected AppPort 8080, got %s", cfg.AppPort)
	}

	if cfg.LogLevel != "info" {
		t.Errorf("Expected LogLevel info, got %s", cfg.LogLevel)
	}

	if cfg.CacheTTL != 5*time.Minute {
		t.Errorf("Expected CacheTTL 5m, got %v", cfg.CacheTTL)
	}
}

func TestLoadConfig_EnvOverride(t *testing.T) {
	// Clear and set environment
	os.Clearenv()
	if err := os.Setenv("APP_PORT", "9000"); err != nil {
		t.Fatalf("failed to set APP_PORT: %v", err)
	}
	if err := os.Setenv("LOG_LEVEL", "debug"); err != nil {
		t.Fatalf("failed to set LOG_LEVEL: %v", err)
	}
	if err := os.Setenv("CACHE_TTL_SECONDS", "600"); err != nil {
		t.Fatalf("failed to set CACHE_TTL_SECONDS: %v", err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.AppPort != "9000" {
		t.Errorf("Expected AppPort 9000, got %s", cfg.AppPort)
	}

	if cfg.LogLevel != "debug" {
		t.Errorf("Expected LogLevel debug, got %s", cfg.LogLevel)
	}

	if cfg.CacheTTL != 10*time.Minute {
		t.Errorf("Expected CacheTTL 10m, got %v", cfg.CacheTTL)
	}
}

func TestValidate_InvalidLogLevel(t *testing.T) {
	cfg := &Config{
		DatabaseURL: "postgres://localhost",
		RedisURL:    "redis://localhost",
		AppPort:     "8080",
		LogLevel:    "invalid",
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected validation error for invalid LogLevel")
	}
}

func TestValidate_InvalidPoolSizes(t *testing.T) {
	cfg := &Config{
		DatabaseURL: "postgres://localhost",
		RedisURL:    "redis://localhost",
		AppPort:     "8080",
		LogLevel:    "info",
		PoolMinSize: 10,
		PoolMaxSize: 5, // Invalid: max < min
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected validation error for PoolMaxSize < PoolMinSize")
	}
}

// TestValidarAutenticacao cobre CA10 do PRD 0009: segredo ausente/curto e kid
// vazio falham, sem ecoar o valor na mensagem.
func TestValidarAutenticacao(t *testing.T) {
	cfg := &Config{JWTSegredo: "curto-demais-segredo-31-bytes!!", JWTKid: "k1"}
	err := cfg.ValidarAutenticacao()
	if err == nil {
		t.Fatal("segredo de 31 bytes deveria falhar")
	}
	if strings.Contains(err.Error(), cfg.JWTSegredo) {
		t.Error("mensagem de erro não pode conter o segredo")
	}
	if (&Config{JWTKid: "k1"}).ValidarAutenticacao() == nil {
		t.Error("segredo ausente deveria falhar")
	}
	if (&Config{JWTSegredo: strings.Repeat("x", 32)}).ValidarAutenticacao() == nil {
		t.Error("kid vazio deveria falhar")
	}
	if err := (&Config{JWTSegredo: strings.Repeat("x", 32), JWTKid: "k1"}).ValidarAutenticacao(); err != nil {
		t.Errorf("config válida rejeitada: %v", err)
	}
}

// TestValidate_Argon2ForaDosLimites: parâmetros absurdos são recusados no boot.
func TestValidate_Argon2ForaDosLimites(t *testing.T) {
	t.Setenv("ARGON2_MEMORIA_KIB", "1024")
	if _, err := LoadConfig(); err == nil {
		t.Error("ARGON2_MEMORIA_KIB=1024 deveria falhar")
	}
}

// TestValidarPagamento cobre as recusas de boot do checkout (PRD 0024 RF09).
func TestValidarPagamento(t *testing.T) {
	base := Config{Ambiente: "dev", Gateway: "fake", EmailProvedor: "fake"}
	casos := []struct {
		nome   string
		mudar  func(*Config)
		recusa string
	}{
		{"padrão dev + fake", func(*Config) {}, ""},
		{"stripe com chave de teste", func(c *Config) { c.Gateway, c.StripeChave, c.StripeWebhookSegredo = "stripe", "sk_test_x", "whsec_x" }, ""},
		{"stripe com chave restrita de teste", func(c *Config) { c.Gateway, c.StripeChave, c.StripeWebhookSegredo = "stripe", "rk_test_x", "whsec_x" }, ""},
		{"produção com stripe", func(c *Config) {
			c.Ambiente, c.Gateway, c.StripeChave, c.StripeWebhookSegredo = "producao", "stripe", "rk_test_x", "whsec_x"
			c.TokenSegredoV1, c.BaseURLPublica = strings.Repeat("s", 32), "https://m.exemplo"
			c.EmailProvedor, c.ResendChave, c.EmailRemetente = "resend", "re_x", "M <m@x.com>"
		}, ""},
		{"stripe sem segredo do webhook", func(c *Config) { c.Gateway, c.StripeChave = "stripe", "rk_test_x" }, "STRIPE_WEBHOOK_SECRET"},
		{"produção com fake", func(c *Config) { c.Ambiente = "producao" }, "fake"},
		{"stripe sem chave", func(c *Config) { c.Gateway = "stripe" }, "exige"},
		{"chave live", func(c *Config) { c.Gateway, c.StripeChave, c.StripeWebhookSegredo = "stripe", "sk_live_x", "whsec_x" }, "modo de teste"},
		{"chave live mesmo com fake", func(c *Config) { c.StripeChave = "rk_live_x" }, "modo de teste"},
		{"gateway desconhecido", func(c *Config) { c.Gateway = "paypal" }, "MORFEU_GATEWAY"},
		{"ambiente desconhecido", func(c *Config) { c.Ambiente = "prod" }, "AMBIENTE"},
		{"e-mail resend completo", func(c *Config) { c.EmailProvedor, c.ResendChave, c.EmailRemetente = "resend", "re_x", "M <m@x.com>" }, ""},
		{"e-mail resend sem chave", func(c *Config) { c.EmailProvedor, c.EmailRemetente = "resend", "M <m@x.com>" }, "RESEND_API_KEY"},
		{"e-mail provedor desconhecido", func(c *Config) { c.EmailProvedor = "smtp" }, "EMAIL_PROVEDOR"},
		{"e-mail fake em produção", func(c *Config) {
			c.Ambiente, c.Gateway, c.StripeChave, c.StripeWebhookSegredo = "producao", "stripe", "rk_test_x", "whsec_x"
			c.TokenSegredoV1, c.BaseURLPublica = strings.Repeat("s", 32), "https://m.exemplo"
		}, "EMAIL_PROVEDOR=fake"},
		{"segredo do token curto", func(c *Config) { c.TokenSegredoV1 = "curto" }, "INGRESSO_TOKEN_SEGREDO_V1"},
		{"produção sem segredo do token", func(c *Config) {
			c.Ambiente, c.Gateway, c.StripeChave, c.StripeWebhookSegredo, c.TokenSegredoV1, c.BaseURLPublica = "producao", "stripe", "rk_test_x", "whsec_x", "", "https://m.exemplo"
			c.EmailProvedor, c.ResendChave, c.EmailRemetente = "resend", "re_x", "M <m@x.com>"
		}, "INGRESSO_TOKEN_SEGREDO_V1"},
		{"produção com base http", func(c *Config) {
			c.Ambiente, c.Gateway, c.StripeChave, c.StripeWebhookSegredo, c.TokenSegredoV1, c.BaseURLPublica = "producao", "stripe", "rk_test_x", "whsec_x", strings.Repeat("s", 32), "http://m.exemplo"
			c.EmailProvedor, c.ResendChave, c.EmailRemetente = "resend", "re_x", "M <m@x.com>"
		}, "BASE_URL_PUBLICA"},
	}
	for _, c := range casos {
		cfg := base
		c.mudar(&cfg)
		err := cfg.ValidarPagamento()
		if c.recusa == "" && err != nil || c.recusa != "" && (err == nil || !strings.Contains(err.Error(), c.recusa)) {
			t.Errorf("%s: err = %v", c.nome, err)
		}
		if err != nil && strings.Contains(err.Error(), "_x") {
			t.Errorf("%s: a mensagem vaza a chave: %v", c.nome, err)
		}
	}
}

// TestValidarRedis cobre RF04/CA04: produção só aceita redis(s)://:senha@host.
func TestValidarRedis(t *testing.T) {
	casos := []struct {
		nome, ambiente, url, recusa string
	}{
		{"dev com url sem senha", "dev", "redis://localhost:6379/0", ""},
		{"dev com formato legado", "dev", "localhost:6379", ""},
		{"produção com senha", "producao", "redis://:s3nha-longa@redis:6379/0", ""},
		{"produção com rediss e usuário", "producao", "rediss://u:s3nha@redis:6379/1", ""},
		{"produção sem senha", "producao", "redis://redis:6379/0", "senha"},
		{"produção com senha vazia", "producao", "redis://:@redis:6379/0", "senha"},
		{"produção com formato legado", "producao", "redis:6379", "REDIS_URL"},
		{"produção com host:porta", "producao", "localhost:6379", "REDIS_URL"},
		{"produção com esquema errado", "producao", "http://:x@redis:6379", "REDIS_URL"},
	}
	for _, c := range casos {
		cfg := Config{Ambiente: c.ambiente, RedisURL: c.url}
		err := cfg.validarRedis()
		if c.recusa == "" && err != nil || c.recusa != "" && (err == nil || !strings.Contains(err.Error(), c.recusa)) {
			t.Errorf("%s: err = %v", c.nome, err)
		}
		if err != nil && strings.Contains(err.Error(), "s3nha") {
			t.Errorf("%s: a mensagem vaza a senha: %v", c.nome, err)
		}
	}
}

// TestLoadConfig_UrlsDosRoles: sem as URLs novas, tudo cai em DATABASE_URL.
func TestLoadConfig_UrlsDosRoles(t *testing.T) {
	os.Clearenv()
	t.Setenv("DATABASE_URL", "postgres://app@h/db")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseMigrateURL != cfg.DatabaseURL || cfg.DatabasePurgeURL != cfg.DatabaseURL {
		t.Errorf("fallback esperado: migrate=%q purga=%q", cfg.DatabaseMigrateURL, cfg.DatabasePurgeURL)
	}

	t.Setenv("DATABASE_MIGRATE_URL", "postgres://migrator@h/db")
	t.Setenv("DATABASE_PURGE_URL", "postgres://purge@h/db")
	cfg, err = LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseMigrateURL != "postgres://migrator@h/db" || cfg.DatabasePurgeURL != "postgres://purge@h/db" {
		t.Errorf("URLs explícitas ignoradas: migrate=%q purga=%q", cfg.DatabaseMigrateURL, cfg.DatabasePurgeURL)
	}
}

// TestLoadConfig_ProducaoExigeSenhaDoRedis: o boot recusa (CA04).
func TestLoadConfig_ProducaoExigeSenhaDoRedis(t *testing.T) {
	os.Clearenv()
	t.Setenv("AMBIENTE", "producao")
	t.Setenv("REDIS_URL", "redis://redis:6379/0")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("produção com Redis sem senha deveria falhar")
	}
	t.Setenv("REDIS_URL", "redis://:uma-senha-longa@redis:6379/0")
	if _, err := LoadConfig(); err != nil {
		t.Fatalf("produção com senha: %v", err)
	}
}

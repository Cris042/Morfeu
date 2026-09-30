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
	base := Config{Ambiente: "dev", Gateway: "fake"}
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
		}, ""},
		{"stripe sem segredo do webhook", func(c *Config) { c.Gateway, c.StripeChave = "stripe", "rk_test_x" }, "STRIPE_WEBHOOK_SECRET"},
		{"produção com fake", func(c *Config) { c.Ambiente = "producao" }, "fake"},
		{"stripe sem chave", func(c *Config) { c.Gateway = "stripe" }, "exige"},
		{"chave live", func(c *Config) { c.Gateway, c.StripeChave, c.StripeWebhookSegredo = "stripe", "sk_live_x", "whsec_x" }, "modo de teste"},
		{"chave live mesmo com fake", func(c *Config) { c.StripeChave = "rk_live_x" }, "modo de teste"},
		{"gateway desconhecido", func(c *Config) { c.Gateway = "paypal" }, "MORFEU_GATEWAY"},
		{"ambiente desconhecido", func(c *Config) { c.Ambiente = "prod" }, "AMBIENTE"},
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

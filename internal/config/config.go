package config

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// tamanhoMinimoSegredoJWT: HS256 exige chave de pelo menos 256 bits (PRD 0008).
const tamanhoMinimoSegredoJWT = 32

// Config holds application configuration loaded from environment variables
type Config struct {
	DatabaseURL string
	RedisURL    string
	RabbitMQURL string
	LogLevel    string
	AppPort     string
	CacheTTL    time.Duration
	PoolMinSize int
	PoolMaxSize int
	PoolTimeout time.Duration

	// Autenticação (PRD 0009). JWTSegredo nunca tem default nem é logado.
	JWTSegredo        string
	JWTKid            string
	Argon2MemoriaKiB  int
	Argon2Iteracoes   int
	Argon2Paralelismo int
	HashConcorrencia  int

	// TMDB (PRD 0012): opcional — sem ele, só as rotas de TMDB respondem 503.
	// Nunca logado.
	TMDBToken string

	// Ambiente (PRD 0024): "dev" ou "producao" — produção recusa o gateway fake.
	Ambiente string
	// Pagamento (PRD 0024, ADR 0010): Gateway "fake" (padrão: CI, dev,
	// load-test) ou "stripe" (modo de teste). Chaves nunca logadas; sem
	// StripeWebhookSegredo a rota do webhook fica desligada.
	Gateway              string
	StripeChave          string
	StripeWebhookSegredo string
}

// LoadConfig loads configuration from environment variables with defaults
func LoadConfig() (*Config, error) {
	cfg := &Config{
		DatabaseURL: getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/morfeu"),
		RedisURL:    getEnv("REDIS_URL", "redis://localhost:6379/0"),
		// Sem default de credencial real (RNF01 do PRD 0002 — nunca guest/guest
		// hardcoded); o default aqui é só para dev local com o docker-compose
		// deste repo, cujo usuário/senha vêm de .env.docker-compose (gitignored).
		RabbitMQURL: getEnv("RABBITMQ_URL", "amqp://morfeu:morfeu@localhost:5672/"),
		LogLevel:    getEnv("LOG_LEVEL", "info"),
		AppPort:     getEnv("APP_PORT", "8080"),
		PoolMinSize: getEnvInt("POOL_MIN_SIZE", 5),
		PoolMaxSize: getEnvInt("POOL_MAX_SIZE", 25),

		JWTSegredo: getEnv("JWT_SEGREDO", ""),
		JWTKid:     getEnv("JWT_KID", "k1"),
		// Mínimo OWASP (m=19 MiB, t=2, p=1) — recalibrar na VM (E0c-CD).
		Argon2MemoriaKiB:  getEnvInt("ARGON2_MEMORIA_KIB", 19*1024),
		Argon2Iteracoes:   getEnvInt("ARGON2_ITERACOES", 2),
		Argon2Paralelismo: getEnvInt("ARGON2_PARALELISMO", 1),
		HashConcorrencia:  getEnvInt("HASH_CONCORRENCIA", runtime.NumCPU()),
		TMDBToken:         getEnv("TMDB_API_TOKEN", ""),

		Ambiente:             getEnv("AMBIENTE", "dev"),
		Gateway:              getEnv("MORFEU_GATEWAY", "fake"),
		StripeChave:          getEnv("STRIPE_SECRET_KEY", ""),
		StripeWebhookSegredo: getEnv("STRIPE_WEBHOOK_SECRET", ""),
	}

	// Parse cache TTL
	ttlSeconds := getEnvInt("CACHE_TTL_SECONDS", 300)
	cfg.CacheTTL = time.Duration(ttlSeconds) * time.Second

	// Parse pool timeout
	poolTimeoutSeconds := getEnvInt("POOL_TIMEOUT_SECONDS", 30)
	cfg.PoolTimeout = time.Duration(poolTimeoutSeconds) * time.Second

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if c.RedisURL == "" {
		return fmt.Errorf("REDIS_URL is required")
	}
	if c.RabbitMQURL == "" {
		return fmt.Errorf("RABBITMQ_URL is required")
	}
	if c.AppPort == "" {
		return fmt.Errorf("APP_PORT is required")
	}
	if c.LogLevel != "debug" && c.LogLevel != "info" && c.LogLevel != "warn" && c.LogLevel != "error" {
		return fmt.Errorf("LOG_LEVEL must be debug, info, warn, or error")
	}
	if c.PoolMinSize < 1 {
		return fmt.Errorf("POOL_MIN_SIZE must be >= 1")
	}
	if c.PoolMaxSize < c.PoolMinSize {
		return fmt.Errorf("POOL_MAX_SIZE must be >= POOL_MIN_SIZE")
	}
	if c.CacheTTL < 1*time.Second {
		return fmt.Errorf("CACHE_TTL_SECONDS must be >= 1")
	}
	return c.validarArgon2()
}

// validarArgon2 limita os custos do hash a faixas sãs (evita DoS acidental por
// config e overflow na conversão para os tipos do argon2).
func (c *Config) validarArgon2() error {
	if c.Argon2MemoriaKiB < 8*1024 || c.Argon2MemoriaKiB > 1024*1024 {
		return fmt.Errorf("ARGON2_MEMORIA_KIB deve estar entre 8192 e 1048576")
	}
	if c.Argon2Iteracoes < 1 || c.Argon2Iteracoes > 10 {
		return fmt.Errorf("ARGON2_ITERACOES deve estar entre 1 e 10")
	}
	if c.Argon2Paralelismo < 1 || c.Argon2Paralelismo > 16 {
		return fmt.Errorf("ARGON2_PARALELISMO deve estar entre 1 e 16")
	}
	if c.HashConcorrencia < 1 {
		return fmt.Errorf("HASH_CONCORRENCIA deve ser >= 1")
	}

	return nil
}

// ValidarAutenticacao exige o segredo do JWT — chamado só quando o processo
// serve HTTP (-mode=api|all); worker e CLI não precisam dele. A mensagem de
// erro nunca inclui o valor.
func (c *Config) ValidarAutenticacao() error {
	if len(c.JWTSegredo) < tamanhoMinimoSegredoJWT {
		return fmt.Errorf("JWT_SEGREDO ausente ou curto (mínimo %d bytes)", tamanhoMinimoSegredoJWT)
	}
	if c.JWTKid == "" {
		return fmt.Errorf("JWT_KID não pode ser vazio")
	}
	return nil
}

// ValidarPagamento confere a config do checkout — chamado só quando o
// processo serve HTTP. Recusa: ambiente/gateway desconhecidos, gateway fake em
// produção, Stripe sem chave e qualquer chave que não seja de modo de teste
// (o projeto é sandbox — doc.md §10). As mensagens nunca incluem valores.
func (c *Config) ValidarPagamento() error {
	if c.Ambiente != "dev" && c.Ambiente != "producao" {
		return fmt.Errorf("AMBIENTE deve ser dev ou producao")
	}
	if c.Gateway != "fake" && c.Gateway != "stripe" {
		return fmt.Errorf("MORFEU_GATEWAY deve ser fake ou stripe")
	}
	if c.Gateway == "fake" && c.Ambiente == "producao" {
		return fmt.Errorf("MORFEU_GATEWAY=fake é proibido com AMBIENTE=producao")
	}
	chaveDeTeste := strings.HasPrefix(c.StripeChave, "sk_test_") || strings.HasPrefix(c.StripeChave, "rk_test_")
	if c.StripeChave != "" && !chaveDeTeste {
		return fmt.Errorf("STRIPE_SECRET_KEY precisa ser de modo de teste (sk_test_/rk_test_)")
	}
	if c.Gateway == "stripe" && (c.StripeChave == "" || c.StripeWebhookSegredo == "") {
		// Sem o webhook nenhum pagamento real é confirmado (auditoria 0024).
		return fmt.Errorf("MORFEU_GATEWAY=stripe exige STRIPE_SECRET_KEY e STRIPE_WEBHOOK_SECRET")
	}
	return nil
}

// getEnv returns environment variable value or default
func getEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}

// getEnvInt returns environment variable as int or default
func getEnvInt(key string, defaultVal int) int {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultVal
	}

	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return defaultVal
	}

	return value
}

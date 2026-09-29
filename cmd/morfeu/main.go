package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/broker"
	"github.com/mclovin137/morfeu/internal/cache"
	"github.com/mclovin137/morfeu/internal/catalogo"
	catalogodb "github.com/mclovin137/morfeu/internal/catalogo/db"
	"github.com/mclovin137/morfeu/internal/config"
	"github.com/mclovin137/morfeu/internal/health"
	"github.com/mclovin137/morfeu/internal/logger"
	"github.com/mclovin137/morfeu/internal/outbox"
)

// modo de execução do binário único (ADR 0001): api serve HTTP e nunca
// publica (RF04); worker roda só o relay da outbox; all faz as duas coisas
// (default — mantém o walking skeleton de ponta a ponta num só processo).
const (
	modeAPI    = "api"
	modeWorker = "worker"
	modeAll    = "all"
)

func main() {
	// Subcomando criar-filme (RF02): stdlib flag, sem endpoint HTTP, roda
	// fora do ciclo de vida do servidor e sai com o próprio exit code.
	if len(os.Args) > 1 && os.Args[1] == "criar-filme" {
		if err := runCriarFilme(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "criar-filme: %v\n", err)
			os.Exit(1)
		}
		return
	}

	mode := flag.String("mode", modeAll, "modo de execução: api | worker | all")
	flag.Parse()

	if *mode != modeAPI && *mode != modeWorker && *mode != modeAll {
		fmt.Fprintf(os.Stderr, "modo inválido: %s (use api, worker ou all)\n", *mode)
		os.Exit(1)
	}

	runServer(*mode)
}

// runServer sobe o servidor HTTP e, quando mode é worker|all, o relay da
// outbox (RF04) — bloqueia até SIGINT/SIGTERM.
func runServer(mode string) {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	log := logger.NewLogger(cfg.LogLevel)
	defer func() {
		if syncErr := log.Sync(); syncErr != nil {
			fmt.Fprintf(os.Stderr, "failed to sync logger: %v\n", syncErr)
		}
	}()

	log.Info("Starting Morfeu application",
		zap.String("app_port", cfg.AppPort),
		zap.String("log_level", cfg.LogLevel),
		zap.String("mode", mode),
	)

	dbPool, err := createDBPool(cfg, log)
	if err != nil {
		log.ErrorMsg("Failed to create database pool", zap.Error(err))
		os.Exit(1)
	}
	defer dbPool.Close()

	log.Info("Database pool created", zap.Int("min_size", cfg.PoolMinSize), zap.Int("max_size", cfg.PoolMaxSize))

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisURL})
	defer func() {
		if closeErr := redisClient.Close(); closeErr != nil {
			log.ErrorMsg("failed to close redis client", zap.Error(closeErr))
		}
	}()

	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		log.ErrorMsg("Failed to connect to Redis", zap.Error(err))
	} else {
		log.Info("Connected to Redis")
	}

	if err := runMigrations(cfg.DatabaseURL, log); err != nil {
		log.ErrorMsg("Migration failed", zap.Error(err))
		os.Exit(1)
	}
	log.Info("Migrations completed")

	cacheLayer := cache.NewRedisCache(redisClient, log.Logger)
	filmService := catalogo.NewFilmService(catalogodb.New(dbPool), dbPool, cacheLayer, log.Logger)
	filmHandler := catalogo.NewFilmHandler(filmService)
	// Relay + consumer ativos só em worker|all (RF04 da 0002, RF08 da 0005) —
	// api nunca publica nem consome. Sobem antes do HTTP para o health já
	// refletir o broker (RF09).
	var relayWG sync.WaitGroup
	relayCtx, cancelRelay := context.WithCancel(context.Background())
	defer cancelRelay()

	healthHandler := health.NewHealthHandler(dbPool, redisClient)
	var brokerClient *broker.Client
	if mode == modeWorker || mode == modeAll {
		brokerClient = startRelay(relayCtx, cfg.RabbitMQURL, dbPool, &relayWG, log)
		startConsumer(relayCtx, brokerClient, dbPool, &relayWG, log)
		healthHandler = healthHandler.WithBroker(brokerClient)
	}

	e := setupRouter(log, filmHandler, healthHandler)

	go func() {
		if err := e.Start(":" + cfg.AppPort); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.ErrorMsg("Server error", zap.Error(err))
		}
	}()
	log.Info("Server started", zap.String("port", cfg.AppPort))

	waitForShutdown(e, log, cancelRelay, &relayWG, brokerClient)
}

// startRelay conecta ao broker (declarando a topologia, RF07) e sobe a
// goroutine do relay da outbox, registrada em relayWG para o shutdown
// ordenado (RF04). Falha na conexão inicial é fatal: o worker sem broker não
// tem função.
func startRelay(ctx context.Context, rabbitURL string, dbPool *pgxpool.Pool, relayWG *sync.WaitGroup, log *logger.Logger) *broker.Client {
	brokerClient := broker.NewClient(rabbitURL, log.Logger)
	if err := brokerClient.Start(ctx); err != nil {
		log.ErrorMsg("Failed to connect to RabbitMQ", zap.Error(err))
		os.Exit(1)
	}
	log.Info("Connected to RabbitMQ, topologia declarada")

	relay := outbox.NewRelay(dbPool, brokerClient, log.Logger)
	relayWG.Add(1)
	go func() {
		defer relayWG.Done()
		relay.Run(ctx)
	}()
	log.Info("Relay da outbox iniciado")

	return brokerClient
}

// startConsumer sobe a goroutine que consome catalogo.filme_criado com dedup
// transacional (RF05/RF08 do PRD 0005), registrada no mesmo WaitGroup do
// relay: no shutdown a entrega em curso termina antes do broker fechar.
func startConsumer(ctx context.Context, brokerClient *broker.Client, dbPool *pgxpool.Pool, wg *sync.WaitGroup, log *logger.Logger) {
	handler := outbox.NovoHandler(dbPool, catalogo.ConsumidorProjecaoFilmes, catalogo.ProjetarFilmeCriado, log.Logger)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := brokerClient.Consumir(ctx, broker.QueueFilmeCriado, handler); err != nil {
			log.ErrorMsg("consumer encerrado com erro", zap.Error(err))
		}
	}()
	log.Info("Consumer de catalogo.filme_criado iniciado")
}

// setupRouter creates the Echo instance, wiring middleware and routes.
func setupRouter(log *logger.Logger, filmHandler *catalogo.FilmHandler, healthHandler *health.HealthHandler) *echo.Echo {
	e := echo.New()

	e.Use(middleware.RecoverWithConfig(middleware.RecoverConfig{
		StackSize: 1 << 10, // 1 KB
	}))
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogMethod:  true,
		LogURI:     true,
		LogStatus:  true,
		LogLatency: true,
		LogValuesFunc: func(_ echo.Context, v middleware.RequestLoggerValues) error {
			log.Info("request",
				zap.String("method", v.Method),
				zap.String("uri", v.URI),
				zap.Int("status", v.Status),
				zap.Duration("latency", v.Latency),
			)
			return nil
		},
	}))

	e.GET("/health", healthHandler.Check)
	e.GET("/filmes", filmHandler.ListFilms)

	return e
}

// waitForShutdown blocks until a termination signal arrives, then shuts down
// the server (and, se ativo, o relay + a conexão com o broker) gracefully
// within a fixed timeout. RF04: para o polling e o consumo, espera a publicação/entrega
// em curso terminar (relayWG.Wait), fecha a conexão — sem goroutine órfã.
func waitForShutdown(e *echo.Echo, log *logger.Logger, cancelRelay context.CancelFunc, relayWG *sync.WaitGroup, brokerClient *broker.Client) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down server")

	cancelRelay()
	relayWG.Wait()
	if brokerClient != nil {
		if err := brokerClient.Close(); err != nil {
			log.ErrorMsg("failed to close broker client", zap.Error(err))
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		log.ErrorMsg("Server shutdown error", zap.Error(err))
		os.Exit(1)
	}

	log.Info("Server stopped")
}

// runCriarFilme implementa o subcomando `morfeu criar-filme` (RF02): cria o
// filme e enfileira catalogo.filme_criado na mesma TX via o service real do
// catálogo, imprime o id criado e retorna erro (exit code != 0) em falha —
// nunca chama os.Exit diretamente, para ficar testável.
func runCriarFilme(args []string) error {
	fs := flag.NewFlagSet("criar-filme", flag.ContinueOnError)
	titulo := fs.String("titulo", "", "título do filme (obrigatório)")
	sinopse := fs.String("sinopse", "", "sinopse do filme")
	duracao := fs.Int("duracao", -1, "duração em minutos")
	ano := fs.Int("ano", -1, "ano de lançamento")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(*titulo) == "" {
		return errors.New("-titulo é obrigatório")
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("carregar config: %w", err)
	}

	log := logger.NewLogger(cfg.LogLevel)
	defer func() { _ = log.Sync() }()

	pool, err := createDBPool(cfg, log)
	if err != nil {
		return fmt.Errorf("conectar ao banco: %w", err)
	}
	defer pool.Close()

	svc := catalogo.NewFilmService(catalogodb.New(pool), pool, nil, log.Logger)

	params := catalogo.CreateFilmParams{Title: strings.TrimSpace(*titulo)}
	if s := strings.TrimSpace(*sinopse); s != "" {
		params.Synopsis = &s
	}
	if *duracao >= 0 {
		r, convErr := safeIntToInt32("duracao", *duracao)
		if convErr != nil {
			return convErr
		}
		params.Runtime = &r
	}
	if *ano >= 0 {
		y, convErr := safeIntToInt32("ano", *ano)
		if convErr != nil {
			return convErr
		}
		params.Year = &y
	}

	film, err := svc.CreateFilm(context.Background(), params)
	if err != nil {
		return fmt.Errorf("criar filme: %w", err)
	}

	fmt.Println(film.ID)
	return nil
}

// safeIntToInt32 converts an int config value to int32, validating the range
// to avoid a silent overflow conversion (gosec G115).
func safeIntToInt32(name string, v int) (int32, error) {
	if v < math.MinInt32 || v > math.MaxInt32 {
		return 0, fmt.Errorf("%s out of int32 range: %d", name, v)
	}
	return int32(v), nil
}

// createDBPool creates a PostgreSQL connection pool
func createDBPool(cfg *config.Config, log *logger.Logger) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	minConns, err := safeIntToInt32("PoolMinSize", cfg.PoolMinSize)
	if err != nil {
		return nil, err
	}
	maxConns, err := safeIntToInt32("PoolMaxSize", cfg.PoolMaxSize)
	if err != nil {
		return nil, err
	}
	poolConfig.MinConns = minConns
	poolConfig.MaxConns = maxConns
	poolConfig.MaxConnLifetime = time.Minute * 15
	poolConfig.MaxConnIdleTime = time.Minute * 5
	poolConfig.ConnConfig.ConnectTimeout = cfg.PoolTimeout

	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test connection
	if err := pool.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return pool, nil
}

// runMigrations runs pending database migrations
func runMigrations(databaseURL string, log *logger.Logger) error {
	m, err := migrate.New("file://migrations", databaseURL)
	if err != nil {
		return fmt.Errorf("failed to create migration instance: %w", err)
	}
	defer func() {
		if sourceErr, dbErr := m.Close(); sourceErr != nil || dbErr != nil {
			log.Warn("failed to close migration instance",
				zap.Error(sourceErr),
				zap.NamedError("database_error", dbErr),
			)
		}
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	version, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		log.Info("No migrations applied")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get migration version: %w", err)
	}

	log.Info("Migrations applied", zap.Uint("version", version), zap.Bool("dirty", dirty))
	return nil
}

package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/autenticacao"
	"github.com/mclovin137/morfeu/internal/broker"
	"github.com/mclovin137/morfeu/internal/cache"
	"github.com/mclovin137/morfeu/internal/catalogo"
	catalogodb "github.com/mclovin137/morfeu/internal/catalogo/db"
	"github.com/mclovin137/morfeu/internal/catalogo/tmdb"
	"github.com/mclovin137/morfeu/internal/config"
	"github.com/mclovin137/morfeu/internal/health"
	"github.com/mclovin137/morfeu/internal/identidade"
	identidadedb "github.com/mclovin137/morfeu/internal/identidade/db"
	"github.com/mclovin137/morfeu/internal/logger"
	"github.com/mclovin137/morfeu/internal/notificacao"
	"github.com/mclovin137/morfeu/internal/outbox"
	"github.com/mclovin137/morfeu/internal/pedido"
	"github.com/mclovin137/morfeu/internal/pedido/pagamento"
	"github.com/mclovin137/morfeu/internal/reserva"
	"github.com/mclovin137/morfeu/internal/sessao"
	sessaodb "github.com/mclovin137/morfeu/internal/sessao/db"
	"github.com/mclovin137/morfeu/internal/telemetria"
)

// versao é sobrescrita no build (-ldflags "-X main.versao=<sha>"); vai para o
// resource OTel (service.version).
var versao = "dev"

// runReplayDLQ: replay-dlq -fila <dlq> [-limite N] [-dry-run]. Imprime só
// contagens e message_ids (nunca payload); dry-run lista sem publicar.
func runReplayDLQ(args []string) error {
	fs := flag.NewFlagSet("replay-dlq", flag.ContinueOnError)
	fila := fs.String("fila", "", "DLQ de origem: "+strings.Join(filasReplay(), " | "))
	limite := fs.Int("limite", 10, "máximo de mensagens nesta execução")
	dryRun := fs.Bool("dry-run", false, "só lista as mensagens, sem republicar")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	log := logger.NewLogger(cfg.LogLevel)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	cli := broker.NewClient(cfg.RabbitMQURL, log.Logger)
	if err := cli.Start(ctx); err != nil {
		return err
	}
	defer func() { _ = cli.Close() }()
	r, err := cli.Reprocessar(ctx, *fila, *limite, *dryRun)
	fmt.Printf("fila=%s dry_run=%t lidas=%d republicadas=%d\n", *fila, *dryRun, r.Lidas, r.Republicadas)
	for _, id := range r.MessageIDs {
		fmt.Println(id)
	}
	return err
}

func filasReplay() []string {
	out := make([]string, 0, len(broker.FilasReplay))
	for f := range broker.FilasReplay {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

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
	// Subcomando seed-operador (RF05 do PRD 0009): único caminho que cria
	// operador — nenhuma rota HTTP faz isso.
	if len(os.Args) > 1 && os.Args[1] == "seed-operador" {
		if err := runSeedOperador(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "seed-operador: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Subcomando replay-dlq (PRD 0027): devolve mensagens de uma DLQ à fila de
	// origem. Só pelo shell da VM — nenhuma superfície HTTP (refinamento E6).
	if len(os.Args) > 1 && os.Args[1] == "replay-dlq" {
		if err := runReplayDLQ(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "replay-dlq: %v\n", err)
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

	tel := iniciarTelemetria(log)
	defer encerrarTelemetria(tel, log)

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
	catalogoServico := catalogo.NovoServico(catalogodb.New(dbPool), dbPool, cacheLayer, log.Logger)
	conectarTMDB(catalogoServico, cfg, log)
	catalogoHandler := catalogo.NovoHandler(catalogoServico, log.Logger)
	// Relay + consumer ativos só em worker|all (RF04 da 0002, RF08 da 0005) —
	// api nunca publica nem consome. Sobem antes do HTTP para o health já
	// refletir o broker (RF09).
	var relayWG sync.WaitGroup
	relayCtx, cancelRelay := context.WithCancel(context.Background())
	defer cancelRelay()

	healthHandler := health.NewHealthHandler(dbPool, redisClient)
	var brokerClient *broker.Client
	if mode == modeWorker || mode == modeAll {
		brokerClient = startWorker(relayCtx, cfg, dbPool, &relayWG, log)
		healthHandler = healthHandler.WithBroker(brokerClient)
	}

	registrarMetricasMensageria(tel, dbPool, brokerClient, log)

	e := setupRouter(log, tel, healthHandler)
	pedidoServico, sessaoServico := registrarRotasDeDominio(e, mode, cfg, dbPool, redisClient, cacheLayer, catalogoServico, catalogoHandler, log)
	iniciarRotinasDaSaga(relayCtx, mode, cfg, brokerClient, dbPool, fonteDoEmail{pedidoServico, sessaoServico, catalogoServico}, &relayWG, log)

	go func() {
		if err := e.Start(":" + cfg.AppPort); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.ErrorMsg("Server error", zap.Error(err))
		}
	}()
	log.Info("Server started", zap.String("port", cfg.AppPort))

	waitForShutdown(e, log, cancelRelay, &relayWG, brokerClient)
}

// iniciarTelemetria sobe traces + métricas (RF01/RF02, PRD 0006); falha é
// fatal — sem providers o processo perderia a observabilidade em silêncio.
func iniciarTelemetria(log *logger.Logger) *telemetria.Telemetria {
	tel, err := telemetria.Iniciar(context.Background(), telemetria.Config{
		Servico:        "morfeu",
		Versao:         versao,
		TaxaAmostragem: telemetria.TaxaAmostragemPadrao,
	})
	if err != nil {
		log.ErrorMsg("Failed to start telemetry", zap.Error(err))
		os.Exit(1)
	}
	return tel
}

func encerrarTelemetria(tel *telemetria.Telemetria, log *logger.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tel.Shutdown(ctx); err != nil {
		log.ErrorMsg("failed to shutdown telemetry", zap.Error(err))
	}
}

// registrarMetricasMensageria liga as fontes reais aos gauges de mensageria
// (RF04, PRD 0006); a DLQ só é medida quando o processo tem broker.
func registrarMetricasMensageria(tel *telemetria.Telemetria, dbPool *pgxpool.Pool, brokerClient *broker.Client, log *logger.Logger) {
	fontes := telemetria.FontesMensageria{
		Pendentes:   func(ctx context.Context) (int64, error) { return outbox.Pendentes(ctx, dbPool) },
		LagSegundos: func(ctx context.Context) (float64, error) { return outbox.LagSegundos(ctx, dbPool) },
	}
	if brokerClient != nil {
		// Todas as DLQs do replay (PRD 0027): uma série por fila.
		fontes.FilasDLQ = filasReplay()
		fontes.ProfundidadeDLQ = func(_ context.Context, fila string) (int, error) {
			return brokerClient.ProfundidadeFila(fila)
		}
	}
	if err := tel.RegistrarMensageria(fontes, log.Logger); err != nil {
		log.ErrorMsg("Failed to register messaging metrics", zap.Error(err))
		os.Exit(1)
	}
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

// conectarTMDB liga o adapter do TMDB ao catálogo quando há token (PRD
// 0012). Sem token o cadastro manual segue normal e as rotas de TMDB dão 503.
func conectarTMDB(s *catalogo.Servico, cfg *config.Config, log *logger.Logger) {
	if cfg.TMDBToken == "" {
		log.Info("TMDB_API_TOKEN ausente — importação do TMDB desativada")
		return
	}
	cliente, err := tmdb.NovoCliente(tmdb.Config{Token: cfg.TMDBToken}, log.Logger)
	if err != nil {
		log.ErrorMsg("cliente TMDB", zap.Error(err))
		os.Exit(1)
	}
	s.ComFonteTMDB(cliente)
}

// registrarRotasDeDominio monta as rotas dos módulos. Cartaz público sempre;
// /auth/* e o backoffice só onde o processo serve a API (api|all), pois
// exigem JWT_SEGREDO (PRD 0009) e o papel operador (PRD 0011).
func registrarRotasDeDominio(e *echo.Echo, mode string, cfg *config.Config, dbPool *pgxpool.Pool, redisClient redis.Cmdable, cacheLayer cache.Cache, catalogoServico *catalogo.Servico, catalogoHandler *catalogo.Handler, log *logger.Logger) (*pedido.Servico, *sessao.Servico) {
	catalogoHandler.RegistrarRotasPublicas(e)
	sessaoHandler, sessaoServico := montarSessao(dbPool, catalogoServico, cacheLayer, log)
	sessaoHandler.RegistrarRotasPublicas(e)
	// Reserva e pedido também existem no worker: as tarefas da saga (PRD 0025)
	// usam o serviço do pedido e a porta transacional da reserva.
	reservaHandler, reservaServico := montarReserva(dbPool, sessaoServico, redisClient, cacheLayer, log)
	pedidoHandler, pedidoServico := montarPedido(cfg, dbPool, sessaoServico, reservaServico, redisClient, log)
	if mode == modeWorker {
		return pedidoServico, sessaoServico
	}
	identidadeHandler, emissor := montarIdentidade(cfg, dbPool, redisClient, log)
	identidadeHandler.RegistrarRotas(e)
	exigirOperador := autenticacao.Exigir(emissor, autenticacao.PapelOperador)
	catalogoHandler.RegistrarRotasBackoffice(e, exigirOperador)
	sessaoHandler.RegistrarRotasBackoffice(e, exigirOperador, operadorDaRequisicao)
	reservaHandler.RegistrarRotas(e)
	pedidoHandler.RegistrarRotas(e)
	return pedidoServico, sessaoServico
}

// operadorDaRequisicao identifica o operador para o log de auditoria mínima
// do módulo sessao (PRD 0013 RF07) sem o módulo importar autenticação.
func operadorDaRequisicao(c echo.Context) string {
	id, _ := autenticacao.UsuarioID(c)
	return id.String()
}

// montarSessao liga o módulo sessao: porta de filmes = catálogo (ADR 0003),
// métrica de conflitos criada aqui (o domínio não conhece OTel).
func montarSessao(dbPool *pgxpool.Pool, filmes sessao.FonteFilmes, c cache.Cache, log *logger.Logger) (*sessao.Handler, *sessao.Servico) {
	conflitos, err := otel.Meter("morfeu/sessao").Int64Counter("sessao_conflitos_total",
		metric.WithDescription("Tentativas de sessão rejeitadas por conflito de horário na sala."))
	if err != nil {
		log.ErrorMsg("métrica de conflitos de sessão", zap.Error(err))
		os.Exit(1)
	}
	servico, err := sessao.NovoServico(sessaodb.New(dbPool), sessao.Config{
		Filmes:     filmes,
		AoConflito: func(ctx context.Context) { conflitos.Add(ctx, 1) },
		Cache:      c,
	}, log.Logger)
	if err != nil {
		log.ErrorMsg("serviço de sessões", zap.Error(err))
		os.Exit(1)
	}
	return sessao.NovoHandler(servico, log.Logger), servico
}

// Rate limit da trava (decisão do usuário no refinamento E4, PRD 0015 RF08).
const (
	limiteTravasIP   = 30
	limiteTravasDono = 20
	janelaTravas     = time.Minute
)

// montarReserva liga o módulo reserva: porta de assentos = sessao (ADR 0003),
// limitadores do E1 (Redis + fallback em memória), métricas (PRD 0015) e o
// cache da ocupação (PRD 0016).
func montarReserva(dbPool *pgxpool.Pool, sessoes reserva.FonteSessoes, redisClient redis.Cmdable, c cache.Cache, log *logger.Logger) (*reserva.Handler, *reserva.Servico) {
	fatal := func(msg string, err error) {
		log.ErrorMsg(msg, zap.Error(err))
		os.Exit(1)
	}
	novoLimitador := func(escopo string, maxim int) *autenticacao.Limitador {
		l, err := autenticacao.NovoLimitador(autenticacao.ConfigLimitador{
			Redis: redisClient, Prefixo: prefixoLimitadorRdb + escopo + ":", Max: maxim, Janela: janelaTravas,
		}, log.Logger)
		if err != nil {
			fatal("limitador "+escopo, err)
		}
		return l
	}
	servico, err := reserva.NovoServico(dbPool, reserva.Config{
		Sessoes:    sessoes,
		LimiteIP:   novoLimitador("trava-ip", limiteTravasIP),
		LimiteDono: novoLimitador("trava-dono", limiteTravasDono),
		Metricas:   metricasReserva(log),
		Cache:      c,
	}, log.Logger)
	if err != nil {
		fatal("serviço de reserva", err)
	}
	return reserva.NovoHandler(servico, log.Logger), servico
}

// iniciarRotinasDaSaga sobe, no worker, as tarefas do pedido (PRD 0025) e o
// consumidor de notificação (PRD 0028; só com broker).
func iniciarRotinasDaSaga(ctx context.Context, mode string, cfg *config.Config, cli *broker.Client, dbPool *pgxpool.Pool, f fonteDoEmail, wg *sync.WaitGroup, log *logger.Logger) {
	iniciarTarefasPedido(ctx, mode, f.pedidos, wg)
	if cli != nil {
		iniciarNotificacao(ctx, cfg, cli, dbPool, f, wg, log)
	}
}

// iniciarNotificacao consome pedido.confirmado (PRD 0026/0028): carrega o
// pedido pelas portas, monta o e-mail com os QRs e envia. O provedor real
// entra na task 0029; até lá, fake (nenhum e-mail sai).
func iniciarNotificacao(ctx context.Context, cfg *config.Config, cli *broker.Client, dbPool *pgxpool.Pool, fonte fonteDoEmail, wg *sync.WaitGroup, log *logger.Logger) {
	fatal := func(msg string, err error) {
		log.ErrorMsg(msg, zap.Error(err))
		os.Exit(1)
	}
	latencia, err := otel.Meter("morfeu/notificacao").Float64Histogram("checkout_confirmado_ate_notificado_segundos",
		// Sem WithUnit: o exporter anexaria "_seconds" ao nome já em PT.
		metric.WithDescription("Pagamento confirmado → notificação processada (SLI do checkout fim a fim). "+
			"O instante do evento tem resolução de segundo: a medida superestima em até 1 s."),
		// Buckets finos em torno do alvo de 5 s (os padrões do OTel pulam de 0 a 5).
		metric.WithExplicitBucketBoundaries(0.5, 1, 2, 3, 5, 10, 30, 60))
	if err != nil {
		fatal("métrica de latência da notificação", err)
	}
	sender, provedor := montarEmailSender(cfg, log)
	entregador, err := notificacao.NovoEntregador(fonte, sender, cfg.BaseURLPublica)
	if err != nil {
		fatal("entregador de e-mail", err)
	}
	meter := otel.Meter("morfeu/notificacao")
	envios, err1 := meter.Int64Counter("morfeu_email_envios_total",
		metric.WithDescription("Entregas de e-mail por provedor, tipo e resultado (ok|ignorado|transitorio|permanente|cota)."))
	duracao, err2 := meter.Float64Histogram("morfeu_email_envio_duracao_segundos",
		// Sem WithUnit: o exporter anexaria "_seconds" ao nome já em PT.
		metric.WithDescription("Duração de cada entrega de e-mail (carregar + montar + enviar)."),
		metric.WithExplicitBucketBoundaries(0.1, 0.25, 0.5, 1, 2, 5, 10))
	if err := errors.Join(err1, err2); err != nil {
		fatal("métricas de e-mail", err)
	}
	aviso, err := notificacao.NovoAvisoDeEstorno(fonte, sender)
	if err != nil {
		fatal("aviso de estorno", err)
	}
	resultado := func(ctx context.Context, tipo, r string, d time.Duration) {
		envios.Add(ctx, 1, metric.WithAttributes(attribute.String("provedor", provedor),
			attribute.String("tipo", tipo), attribute.String("resultado", r)))
		duracao.Record(ctx, d.Seconds(), metric.WithAttributes(attribute.String("provedor", provedor)))
	}
	// As séries do alerta de recusa/cota nascem em 0 (o increase() não vê a 1ª).
	for _, tipo := range []string{notificacao.TipoConfirmacao, notificacao.TipoEstorno} {
		for _, r := range []string{notificacao.ResultadoPermanente, notificacao.ResultadoCota} {
			envios.Add(context.Background(), 0, metric.WithAttributes(attribute.String("provedor", provedor),
				attribute.String("tipo", tipo), attribute.String("resultado", r)))
		}
	}
	consumir := func(fila, consumidor string, c *notificacao.ConsumidorPedidos) {
		handler := outbox.NovoHandler(dbPool, consumidor, c.Efeito, log.Logger)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := cli.Consumir(ctx, fila, handler); err != nil {
				log.ErrorMsg("consumer encerrado com erro", zap.String("fila", fila), zap.Error(err))
			}
		}()
		log.Info("Consumer de notificação iniciado", zap.String("fila", fila))
	}
	consumir(broker.QueuePedidoConfirmado, notificacao.Consumidor, notificacao.NovoConsumidor(notificacao.Config{
		Latencia: func(ctx context.Context, d time.Duration) { latencia.Record(ctx, d.Seconds()) },
		Entregar: entregador.Entregar, Resultado: resultado,
	}, log.Logger))
	// Estorno (PRD 0030): sem latência de checkout — não é o SLI da compra.
	consumir(broker.QueuePedidoEstornado, notificacao.ConsumidorEstorno, notificacao.NovoConsumidor(notificacao.Config{
		Tipo: notificacao.TipoEstorno, Entregar: aviso.Entregar, Resultado: resultado,
	}, log.Logger))
}

// montarEmailSender escolhe o provedor (ADR 0005 Strategy): Resend ou o
// fake (dev/CI/load-test — o boot já recusou fake em produção).
func montarEmailSender(cfg *config.Config, log *logger.Logger) (notificacao.EmailSender, string) {
	if cfg.EmailProvedor != "resend" {
		log.Warn("envio de e-mail FAKE ativo (nenhum e-mail sai)")
		return notificacao.NovoFake(), "fake"
	}
	r, err := notificacao.NovoResend(notificacao.ConfigResend{Chave: cfg.ResendChave, Remetente: cfg.EmailRemetente})
	if err != nil {
		log.ErrorMsg("provedor de e-mail", zap.Error(err))
		os.Exit(1)
	}
	return r, "resend"
}

// fonteDoEmail compõe as portas de pedido, sessão e catálogo para a
// notificação (ADR 0003: nenhum módulo lê tabela alheia; o main traduz os
// erros para o vocabulário da notificação).
type fonteDoEmail struct {
	pedidos  *pedido.Servico
	sessoes  *sessao.Servico
	catalogo *catalogo.Servico
}

func (f fonteDoEmail) Carregar(ctx context.Context, id uuid.UUID) (notificacao.DadosEmail, error) {
	d, err := f.pedidos.DadosParaNotificacao(ctx, id)
	switch {
	case errors.Is(err, pedido.ErrNaoNotificavel):
		return notificacao.DadosEmail{}, notificacao.ErrNaoNotificavel
	case errors.Is(err, pedido.ErrPedidoNaoEncontrado):
		return notificacao.DadosEmail{}, notificacao.ErrPedidoInexistente
	case err != nil:
		return notificacao.DadosEmail{}, err
	}
	s, ok, err := f.sessoes.DadosParaIngresso(ctx, d.SessaoID)
	if err != nil || !ok {
		return notificacao.DadosEmail{}, fmt.Errorf("sessão %d do pedido: ok=%t: %w", d.SessaoID, ok, err)
	}
	titulo, ok, err := f.catalogo.TituloDoFilme(ctx, s.FilmeID)
	if err != nil || !ok {
		return notificacao.DadosEmail{}, fmt.Errorf("filme %d da sessão: ok=%t: %w", s.FilmeID, ok, err)
	}
	out := notificacao.DadosEmail{PedidoID: id, Para: d.Email, Codigo: d.Codigo, Filme: titulo,
		Inicio: s.Inicio, Sala: s.Sala, TotalCentavos: d.TotalCentavos}
	for _, i := range d.Ingressos {
		out.Ingressos = append(out.Ingressos, notificacao.IngressoEmail{ID: i.ID, Assento: i.Assento, Token: i.Token})
	}
	return out, nil
}

// CarregarEstorno compõe o aviso de estorno (PRD 0030) a partir do pedido.
func (f fonteDoEmail) CarregarEstorno(ctx context.Context, id uuid.UUID) (notificacao.DadosEstorno, error) {
	d, err := f.pedidos.DadosParaAvisoDeEstorno(ctx, id)
	switch {
	case errors.Is(err, pedido.ErrNaoNotificavel):
		return notificacao.DadosEstorno{}, notificacao.ErrNaoNotificavel
	case errors.Is(err, pedido.ErrPedidoNaoEncontrado):
		return notificacao.DadosEstorno{}, notificacao.ErrPedidoInexistente
	case err != nil:
		return notificacao.DadosEstorno{}, err
	}
	return notificacao.DadosEstorno{PedidoID: id, Para: d.Email, Codigo: d.Codigo, TotalCentavos: d.TotalCentavos}, nil
}

// iniciarTarefasPedido roda reconciliação + estornos da saga (PRD 0025) em
// -mode=worker|all, no mesmo WaitGroup do shutdown gracioso.
func iniciarTarefasPedido(ctx context.Context, mode string, s *pedido.Servico, wg *sync.WaitGroup) {
	if mode != modeWorker && mode != modeAll {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.RodarTarefas(ctx, pedido.IntervaloTarefas)
	}()
}

// Rate limit da criação de pedido e da rota do webhook (refinamento E6).
const (
	limitePedidosIP   = 10
	limitePedidosDono = 5
	limiteWebhookIP   = 300
)

// montarPedido liga o módulo pedido (PRD 0023/0024): porta de preço =
// sessao, porta transacional = reserva (ADR 0010), gateway escolhido pela
// config (fake recusado em produção), verificador do webhook, limitadores do
// E1, funil e métricas do gateway.
func montarPedido(cfg *config.Config, dbPool *pgxpool.Pool, sessoes pedido.FonteSessoes, r *reserva.Servico, redisClient redis.Cmdable, log *logger.Logger) (*pedido.Handler, *pedido.Servico) {
	fatal := func(msg string, err error) {
		log.ErrorMsg(msg, zap.Error(err))
		os.Exit(1)
	}
	if err := cfg.ValidarPagamento(); err != nil {
		fatal("configuração de pagamento inválida", err)
	}
	novoLimitador := func(escopo string, maxim int) *autenticacao.Limitador {
		l, err := autenticacao.NovoLimitador(autenticacao.ConfigLimitador{
			Redis: redisClient, Prefixo: prefixoLimitadorRdb + escopo + ":", Max: maxim, Janela: time.Minute,
		}, log.Logger)
		if err != nil {
			fatal("limitador "+escopo, err)
		}
		return l
	}
	meter := otel.Meter("morfeu/pedido")
	funil, err := meter.Int64Counter("checkout_funil_total",
		metric.WithDescription("Pedidos por etapa do funil do checkout (etapas fixas)."))
	if err != nil {
		fatal("métrica do funil", err)
	}
	compensacoes, err := meter.Int64Counter("saga_compensacoes_total",
		metric.WithDescription("Compensações da saga do checkout por passo (cobranca|estorno) — disparo = problema no checkout."))
	if err != nil {
		fatal("métrica de compensações", err)
	}
	pc := pedido.Config{
		SegredosToken: map[int16][]byte{1: segredoToken(cfg, log)},
		Sessoes:       sessoes,
		Reserva:       reservaDoPedido{r},
		Gateway:       montarGateway(cfg, meter, log),
		LimiteIP:      novoLimitador("pedido-ip", limitePedidosIP),
		LimiteDono:    novoLimitador("pedido-dono", limitePedidosDono),
		Funil: func(ctx context.Context, etapa string) {
			funil.Add(ctx, 1, metric.WithAttributes(attribute.String("etapa", etapa)))
		},
		Compensacao: func(ctx context.Context, passo string) {
			compensacoes.Add(ctx, 1, metric.WithAttributes(attribute.String("passo", passo)))
		},
	}
	if cfg.StripeWebhookSegredo == "" {
		log.Warn("STRIPE_WEBHOOK_SECRET ausente: rota /webhooks/stripe desligada")
	} else {
		if pc.Webhook, err = pagamento.NovoWebhook(cfg.StripeWebhookSegredo); err != nil {
			fatal("webhook do gateway", err)
		}
		pc.LimiteWebhook = novoLimitador("webhook-ip", limiteWebhookIP)
	}
	servico, err := pedido.NovoServico(dbPool, pc, log.Logger)
	if err != nil {
		fatal("serviço de pedido", err)
	}
	if err := registrarPedidosPresos(meter, servico); err != nil {
		fatal("métrica de pedidos presos", err)
	}
	// As séries nascem em 0: sem isso o increase() não enxerga o 1º estorno.
	for _, passo := range []string{pedido.PassoCobranca, pedido.PassoEstorno} {
		compensacoes.Add(context.Background(), 0, metric.WithAttributes(attribute.String("passo", passo)))
	}
	return pedido.NovoHandler(servico, log.Logger), servico
}

// segredoToken devolve o segredo do HMAC do ingresso (versão 1). Em dev sem
// INGRESSO_TOKEN_SEGREDO_V1, gera um aleatório por processo (links do e-mail
// deixam de valer após reiniciar); em produção o boot já recusou a ausência.
func segredoToken(cfg *config.Config, log *logger.Logger) []byte {
	if cfg.TokenSegredoV1 != "" {
		return []byte(cfg.TokenSegredoV1)
	}
	b := make([]byte, pedido.TamanhoMinimoSegredoToken)
	if _, err := rand.Read(b); err != nil {
		log.ErrorMsg("gerar segredo do token", zap.Error(err))
		os.Exit(1)
	}
	log.Warn("INGRESSO_TOKEN_SEGREDO_V1 ausente: segredo aleatório por processo (só dev)")
	return b
}

// registrarPedidosPresos expõe pedidos_presos{estado} (PRD 0026): lido do
// banco a cada coleta — índices parciais mantêm a contagem barata.
func registrarPedidosPresos(meter metric.Meter, s *pedido.Servico) error {
	presos, err := meter.Int64ObservableGauge("pedidos_presos",
		metric.WithDescription("Pedidos que a saga deveria ter resolvido: pendentes vencidos além da margem e estornos pendentes."))
	if err != nil {
		return err
	}
	vencido := metric.WithAttributes(attribute.String("estado", "aguardando_vencido"))
	estorno := metric.WithAttributes(attribute.String("estado", "estorno_pendente"))
	_, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		v, e, err := s.Presos(ctx)
		if err != nil {
			return err
		}
		o.ObserveInt64(presos, v, vencido)
		o.ObserveInt64(presos, e, estorno)
		return nil
	}, presos)
	return err
}

// montarGateway escolhe o adapter (ADR 0005 Strategy): Stripe em modo de
// teste ou o fake (CI, dev e load-test). Métricas gateway_* sem labels de
// alta cardinalidade.
func montarGateway(cfg *config.Config, meter metric.Meter, log *logger.Logger) pagamento.Gateway {
	if cfg.Gateway != "stripe" {
		log.Warn("gateway de pagamento FAKE ativo (nenhuma cobrança real)")
		return pagamento.NovoFake()
	}
	requisicoes, err1 := meter.Int64Counter("gateway_requests_total", metric.WithDescription("Chamadas ao gateway por operação e resultado."))
	// Sem WithUnit: o exporter anexaria "_seconds" ao nome (ficaria _seconds_seconds).
	duracao, err2 := meter.Float64Histogram("gateway_duration_seconds", metric.WithDescription("Duração das chamadas ao gateway, em segundos."))
	breaker, err3 := meter.Int64Gauge("gateway_breaker_state", metric.WithDescription("Circuit breaker do gateway (1 = aberto)."))
	if err := errors.Join(err1, err2, err3); err != nil {
		log.ErrorMsg("métricas do gateway", zap.Error(err))
		os.Exit(1)
	}
	gw, err := pagamento.NovoStripe(pagamento.ConfigStripe{
		Chave: cfg.StripeChave,
		Observar: func(op, resultado string, d time.Duration) {
			ctx := context.Background()
			requisicoes.Add(ctx, 1, metric.WithAttributes(attribute.String("op", op), attribute.String("resultado", resultado)))
			duracao.Record(ctx, d.Seconds(), metric.WithAttributes(attribute.String("op", op)))
		},
		AoMudarBreaker: func(aberto bool) {
			v := int64(0)
			if aberto {
				v = 1
				log.Warn("circuit breaker do gateway ABERTO")
			}
			breaker.Record(context.Background(), v)
		},
	})
	if err != nil {
		log.ErrorMsg("gateway Stripe", zap.Error(err))
		os.Exit(1)
	}
	return gw
}

// reservaDoPedido adapta a reserva à porta transacional do pedido (ADR
// 0010): converte o hash do carrinho em reserva.Dono e traduz os erros da
// reserva para o vocabulário do pedido — nenhum módulo importa o outro.
type reservaDoPedido struct{ s *reserva.Servico }

func (a reservaDoPedido) DonoDoToken(token string) ([]byte, bool) {
	d, ok := reserva.DonoDoToken(token)
	if !ok {
		return nil, false
	}
	return d.Hash(), true
}

func (a reservaDoPedido) PrenderParaPedido(ctx context.Context, tx outbox.Tx, donoHash []byte, sessaoID int64, codigos []string, pedidoID uuid.UUID, ate time.Time) error {
	d, ok := reserva.DonoDoHash(donoHash)
	if !ok {
		return pedido.ErrHoldsInvalidos
	}
	err := a.s.PrenderParaPedido(ctx, tx, d, sessaoID, codigos, pedidoID, ate)
	if errors.Is(err, reserva.ErrHoldsDoPedido) || errors.Is(err, reserva.ErrDadosInvalidos) {
		return pedido.ErrHoldsInvalidos
	}
	return err
}

func (a reservaDoPedido) LiberarDoPedido(ctx context.Context, tx outbox.Tx, pedidoID uuid.UUID) (int64, error) {
	return a.s.LiberarDoPedido(ctx, tx, pedidoID)
}

func (a reservaDoPedido) ConverterDoPedido(ctx context.Context, tx outbox.Tx, pedidoID uuid.UUID) ([]string, error) {
	return a.s.ConverterDoPedido(ctx, tx, pedidoID)
}

// metricasReserva cria os contadores da trava (sem labels — PRD 0015 RF10);
// o domínio recebe só callbacks.
func metricasReserva(log *logger.Logger) reserva.Metricas {
	meter := otel.Meter("morfeu/reserva")
	contador := func(nome, descricao string) metric.Int64Counter {
		c, err := meter.Int64Counter(nome, metric.WithDescription(descricao))
		if err != nil {
			log.ErrorMsg("métrica "+nome, zap.Error(err))
			os.Exit(1)
		}
		return c
	}
	criados := contador("reserva_holds_criados_total", "Holds de assento criados (inclui roubo de vencido).")
	indisponiveis := contador("reserva_holds_indisponiveis_total", "Travas recusadas porque outro dono segura o assento.")
	expirados := contador("reserva_holds_expirados_total", "Holds vencidos marcados como expirados pelo sweeper.")
	varreduras := contador("reserva_sweeper_execucoes_total", "Passadas concluídas do sweeper de holds.")
	convertidos := contador("reserva_holds_convertidos_total", "Holds convertidos em venda pelo pedido (PRD 0022).")
	return reserva.Metricas{
		Criados:       func(ctx context.Context, n int64) { criados.Add(ctx, n) },
		Indisponiveis: func(ctx context.Context) { indisponiveis.Add(ctx, 1) },
		Expirados:     func(ctx context.Context, n int64) { expirados.Add(ctx, n) },
		Varreduras:    func(ctx context.Context) { varreduras.Add(ctx, 1) },
		Convertidos:   func(ctx context.Context, n int64) { convertidos.Add(ctx, n) },
	}
}

// startWorker sobe tudo que roda só em -mode=worker|all: relay da outbox,
// consumer, limpeza de refresh e sweeper de holds — todos no mesmo WaitGroup do shutdown.
func startWorker(ctx context.Context, cfg *config.Config, dbPool *pgxpool.Pool, wg *sync.WaitGroup, log *logger.Logger) *broker.Client {
	brokerClient := startRelay(ctx, cfg.RabbitMQURL, dbPool, wg, log)
	startConsumer(ctx, brokerClient, dbPool, wg, log)
	startLimpezaRefresh(ctx, dbPool, wg, log)
	wg.Add(1)
	go func() {
		defer wg.Done()
		outbox.RodarLimpeza(ctx, dbPool, outbox.IntervaloLimpeza, log.Logger)
	}()
	sweeper := reserva.NovoSweeper(dbPool, metricasReserva(log), nil, log.Logger)
	wg.Add(1)
	go func() {
		defer wg.Done()
		sweeper.Rodar(ctx, reserva.IntervaloSweeper)
	}()
	return brokerClient
}

// intervaloLimpezaRefresh: refresh vencidos saem da tabela 1×/hora (RF08 do
// PRD 0010) — volume baixo, sem necessidade de janela especial.
const intervaloLimpezaRefresh = time.Hour

// startLimpezaRefresh roda a limpeza de refresh expirados no worker (sem
// serviço novo), registrada no WaitGroup do shutdown ordenado.
func startLimpezaRefresh(ctx context.Context, dbPool *pgxpool.Pool, wg *sync.WaitGroup, log *logger.Logger) {
	q := identidadedb.New(dbPool)
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(intervaloLimpezaRefresh)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n, err := identidade.LimparRefreshExpirados(ctx, q)
				if err != nil {
					log.ErrorMsg("limpeza de refresh expirados falhou", zap.Error(err))
					continue
				}
				log.Info("refresh expirados removidos", zap.Int64("quantidade", n))
			}
		}
	}()
}

// setupRouter creates the Echo instance, wiring middleware and routes.
func setupRouter(log *logger.Logger, tel *telemetria.Telemetria, healthHandler *health.HealthHandler) *echo.Echo {
	e := echo.New()
	// IP do cliente = endereço da conexão (limitador por IP, PRD 0009 RF09):
	// sem proxy confiável até a E0c-CD, X-Forwarded-For seria forjável.
	e.IPExtractor = echo.ExtractIPDirect()

	// Primeiro middleware: o span/métrica cobre recover e logger (RF03, PRD 0006).
	e.Use(tel.MiddlewareHTTP("morfeu"))

	e.Use(middleware.RecoverWithConfig(middleware.RecoverConfig{
		StackSize: 1 << 10, // 1 KB
	}))
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogMethod:  true,
		LogURI:     true,
		LogStatus:  true,
		LogLatency: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			logger.ComTrace(c.Request().Context(), log.Logger).Info("request",
				zap.String("method", v.Method),
				zap.String("uri", v.URI),
				zap.Int("status", v.Status),
				zap.Duration("latency", v.Latency),
			)
			return nil
		},
	}))

	e.GET("/health", healthHandler.Check)
	// /metrics é interno (scrape do Prometheus na rede do compose, task 0007);
	// o proxy público (E0c-CD) não roteia este path.
	e.GET("/metrics", echo.WrapHandler(tel.Handler()))

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
	duracao := fs.Int("duracao", -1, "duração em minutos (obrigatória, 1–1440)")
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

	servico := catalogo.NovoServico(catalogodb.New(pool), pool, nil, log.Logger)

	dados := catalogo.DadosFilme{Titulo: *titulo}
	if s := strings.TrimSpace(*sinopse); s != "" {
		dados.Sinopse = &s
	}
	if *duracao >= 0 {
		r, convErr := safeIntToInt32("duracao", *duracao)
		if convErr != nil {
			return convErr
		}
		dados.DuracaoMin = &r
	}
	if *ano >= 0 {
		y, convErr := safeIntToInt32("ano", *ano)
		if convErr != nil {
			return convErr
		}
		dados.Ano = &y
	}

	// Mesmo caso de uso do backoffice (RF07): mesmas validações — sem
	// -duracao o filme é recusado (o E3 depende da duração).
	film, err := servico.Criar(context.Background(), dados)
	if err != nil {
		return fmt.Errorf("criar filme: %w", err)
	}

	fmt.Println(film.ID)
	return nil
}

// Limites de tentativas (RN02 do PRD 0008 / RF02–RF03 do PRD 0009).
const (
	limiteFalhasConta   = 5
	limiteFalhasIP      = 20
	janelaFalhasLogin   = 5 * time.Minute
	limiteCadastrosIP   = 10
	janelaCadastrosIP   = time.Hour
	prefixoLimitadorRdb = "morfeu:auth:"
)

// montarIdentidade monta emissor, limitadores (Redis + fallback em memória),
// métricas e o serviço de identidade. Falha de config é fatal: a API não sobe
// sem segredo JWT válido (RF09).
func montarIdentidade(cfg *config.Config, dbPool *pgxpool.Pool, redisClient redis.Cmdable, log *logger.Logger) (*identidade.Handler, *autenticacao.Emissor) {
	fatal := func(msg string, err error) {
		log.ErrorMsg(msg, zap.Error(err))
		os.Exit(1)
	}
	if err := cfg.ValidarAutenticacao(); err != nil {
		fatal("configuração de autenticação inválida", err)
	}
	emissor, err := autenticacao.NovoEmissor(autenticacao.ConfigJWT{
		Segredo: []byte(cfg.JWTSegredo), Kid: cfg.JWTKid, TTL: autenticacao.TTLAccessPadrao,
	})
	if err != nil {
		fatal("emissor JWT", err)
	}
	novoLimitador := func(escopo string, maxim int, janela time.Duration) *autenticacao.Limitador {
		l, lerr := autenticacao.NovoLimitador(autenticacao.ConfigLimitador{
			Redis: redisClient, Prefixo: prefixoLimitadorRdb + escopo + ":", Max: maxim, Janela: janela,
		}, log.Logger)
		if lerr != nil {
			fatal("limitador "+escopo, lerr)
		}
		return l
	}
	metricas, err := autenticacao.NovasMetricas()
	if err != nil {
		fatal("métricas de autenticação", err)
	}
	servico, err := identidade.NovoServico(dbPool, identidadedb.New(dbPool), emissor, identidade.Config{
		Argon2:           parametrosArgon2(cfg),
		HashConcorrencia: cfg.HashConcorrencia,
		LimiteConta:      novoLimitador("conta", limiteFalhasConta, janelaFalhasLogin),
		LimiteIP:         novoLimitador("ip", limiteFalhasIP, janelaFalhasLogin),
		LimiteRegistro:   novoLimitador("registro", limiteCadastrosIP, janelaCadastrosIP),
	}, metricas, log.Logger)
	if err != nil {
		fatal("serviço de identidade", err)
	}
	return identidade.NovoHandler(servico, emissor, log.Logger), emissor
}

// parametrosArgon2 converte a config (faixas já validadas em config.Validate).
func parametrosArgon2(cfg *config.Config) identidade.ParametrosArgon2 {
	return identidade.ParametrosArgon2{
		MemoriaKiB:  uint32(cfg.Argon2MemoriaKiB), //nolint:gosec // G115: 8192..1048576 validado em config.Validate
		Iteracoes:   uint32(cfg.Argon2Iteracoes),  //nolint:gosec // G115: 1..10 validado em config.Validate
		Paralelismo: uint8(cfg.Argon2Paralelismo), //nolint:gosec // G115: 1..16 validado em config.Validate
	}
}

// runSeedOperador implementa `morfeu seed-operador -nome <n> -email <e>`
// (RF05 do PRD 0009): cria o operador com senha aleatória exibida UMA vez no
// stdout (nunca logada); e-mail existente → nada muda (idempotente).
func runSeedOperador(args []string) error {
	fs := flag.NewFlagSet("seed-operador", flag.ContinueOnError)
	nome := fs.String("nome", "", "nome do operador (obrigatório)")
	email := fs.String("email", "", "e-mail do operador (obrigatório)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*nome) == "" || strings.TrimSpace(*email) == "" {
		return errors.New("-nome e -email são obrigatórios")
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

	senha, criado, err := identidade.SeedOperador(context.Background(), identidadedb.New(pool), parametrosArgon2(cfg), log.Logger, *nome, *email)
	if err != nil {
		return fmt.Errorf("criar operador: %w", err)
	}
	if !criado {
		fmt.Println("operador já existe — nada alterado")
		return nil
	}
	fmt.Printf("operador criado. Senha inicial (exibida só agora, guarde-a): %s\n", senha)
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
	// Span por query (RF05, PRD 0006); sem parâmetros nos atributos (padrão).
	poolConfig.ConnConfig.Tracer = otelpgx.NewTracer()

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

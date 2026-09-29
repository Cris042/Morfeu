// Package tmdb é o adapter HTTP do TMDB para o port catalogo.FonteTMDB
// (PRD 0012, ADR 0003 ports & adapters). stdlib net/http, bearer v4, host
// fixo, timeout + retry com backoff — SEM circuit breaker (decisão da
// descoberta: volume ínfimo, só o operador usa). O token nunca aparece em
// erro, log ou span.
package tmdb

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/catalogo"
)

const (
	// hostPadrao é o ÚNICO destino em produção (sem host vindo de input).
	hostPadrao        = "https://api.themoviedb.org"
	timeoutTentativa  = 5 * time.Second
	maxTentativas     = 3
	backoffBase       = 300 * time.Millisecond
	tetoRetryAfter    = 5 * time.Second
	limiteCorpo       = 1 << 20 // 1 MiB: resposta maior é tratada como inválida
	idioma            = "pt-BR"
	maxResultadosBusc = 20
)

// Config parametriza o cliente. BaseURL e Dormir existem para os testes
// (httptest e relógio); em produção ficam vazios.
type Config struct {
	Token   string
	BaseURL string
	Dormir  func(context.Context, time.Duration) error
}

// Cliente implementa catalogo.FonteTMDB.
type Cliente struct {
	token    string
	base     string
	http     *http.Client
	dormir   func(context.Context, time.Duration) error
	logger   *zap.Logger
	chamadas metric.Int64Counter
	duracao  metric.Float64Histogram
}

// NovoCliente cria o adapter; token vazio é erro de configuração.
func NovoCliente(cfg Config, logger *zap.Logger) (*Cliente, error) {
	if cfg.Token == "" {
		return nil, errors.New("tmdb: token ausente")
	}
	base := cfg.BaseURL
	if base == "" {
		base = hostPadrao
	}
	dormir := cfg.Dormir
	if dormir == nil {
		dormir = dormirCtx
	}
	m := otel.Meter("morfeu/tmdb")
	chamadas, err := m.Int64Counter("tmdb_requisicoes_total", metric.WithDescription("Chamadas ao TMDB por operação e classe de status."))
	if err != nil {
		return nil, fmt.Errorf("tmdb: métrica: %w", err)
	}
	duracao, err := m.Float64Histogram("tmdb_requisicao_duracao_segundos", metric.WithDescription("Duração de cada tentativa ao TMDB em segundos."))
	if err != nil {
		return nil, fmt.Errorf("tmdb: métrica: %w", err)
	}
	return &Cliente{
		token: cfg.Token, base: base, http: &http.Client{Timeout: timeoutTentativa},
		dormir: dormir, logger: logger, chamadas: chamadas, duracao: duracao,
	}, nil
}

type detalheJSON struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Overview    string `json:"overview"`
	Runtime     int    `json:"runtime"`
	ReleaseDate string `json:"release_date"`
	PosterPath  string `json:"poster_path"`
	ImdbID      string `json:"imdb_id"`
}

type buscaJSON struct {
	Results []struct {
		ID          int64  `json:"id"`
		Title       string `json:"title"`
		ReleaseDate string `json:"release_date"`
		PosterPath  string `json:"poster_path"`
	} `json:"results"`
}

// Detalhar busca GET /3/movie/{id} em pt-BR.
func (c *Cliente) Detalhar(ctx context.Context, tmdbID int64) (catalogo.DadosTMDB, error) {
	var d detalheJSON
	caminho := "/3/movie/" + strconv.FormatInt(tmdbID, 10) + "?language=" + idioma
	if err := c.obter(ctx, "detalhar", caminho, &d); err != nil {
		return catalogo.DadosTMDB{}, err
	}
	if d.ID != tmdbID {
		return catalogo.DadosTMDB{}, fmt.Errorf("tmdb: resposta de outro filme: %w", catalogo.ErrTMDBIndisponivel)
	}
	return catalogo.DadosTMDB{
		TmdbID: d.ID, Titulo: d.Title, Sinopse: d.Overview, DuracaoMin: d.Runtime,
		DataLanc: d.ReleaseDate, PosterPath: d.PosterPath, ImdbID: d.ImdbID,
	}, nil
}

// Buscar consulta GET /3/search/movie em pt-BR (até 20 resultados).
func (c *Cliente) Buscar(ctx context.Context, termo string) ([]catalogo.ResultadoTMDB, error) {
	var b buscaJSON
	caminho := "/3/search/movie?include_adult=false&language=" + idioma + "&page=1&query=" + url.QueryEscape(termo)
	if err := c.obter(ctx, "buscar", caminho, &b); err != nil {
		return nil, err
	}
	out := make([]catalogo.ResultadoTMDB, 0, len(b.Results))
	for i, r := range b.Results {
		if i == maxResultadosBusc {
			break
		}
		out = append(out, catalogo.NovoResultadoTMDB(r.ID, r.Title, r.ReleaseDate, r.PosterPath))
	}
	return out, nil
}

// obter faz o GET com até maxTentativas: retry só em 5xx, 429 (Retry-After)
// e erro de rede/timeout; 404 → ErrTMDBNaoEncontrado; outro 4xx → sem retry.
func (c *Cliente) obter(ctx context.Context, operacao, caminho string, destino any) error {
	ctx, span := otel.Tracer("morfeu/tmdb").Start(ctx, "tmdb."+operacao)
	defer span.End()

	var ultimoErr error
	for tentativa := 1; tentativa <= maxTentativas; tentativa++ {
		espera, retentar, err := c.tentar(ctx, operacao, caminho, destino)
		if err == nil {
			return nil
		}
		ultimoErr = err
		if !retentar || tentativa == maxTentativas {
			break
		}
		if espera == 0 {
			espera = backoff(tentativa)
		}
		if err := c.dormir(ctx, espera); err != nil {
			return fmt.Errorf("tmdb: %s cancelado: %w", operacao, catalogo.ErrTMDBIndisponivel)
		}
	}
	c.logger.Warn("tmdb: falha após tentativas", zap.String("operacao", operacao), zap.Error(ultimoErr))
	return ultimoErr
}

// tentar executa uma tentativa. Devolve (espera sugerida, pode retentar, erro).
func (c *Cliente) tentar(ctx context.Context, operacao, caminho string, destino any) (time.Duration, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+caminho, nil)
	if err != nil {
		return 0, false, fmt.Errorf("tmdb: montar requisição: %w", catalogo.ErrTMDBIndisponivel)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	inicio := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		c.registrar(ctx, operacao, "erro", inicio)
		// Erro de transporte pode citar a URL (sem credencial: o token vai no
		// header), mas por garantia não propagamos a mensagem crua.
		return 0, true, fmt.Errorf("tmdb: %s: falha de rede/timeout: %w", operacao, catalogo.ErrTMDBIndisponivel)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusOK:
		c.registrar(ctx, operacao, "2xx", inicio)
		corpo, err := io.ReadAll(io.LimitReader(resp.Body, limiteCorpo+1))
		if err != nil || len(corpo) > limiteCorpo {
			return 0, false, fmt.Errorf("tmdb: %s: corpo ilegível: %w", operacao, catalogo.ErrTMDBIndisponivel)
		}
		if err := json.Unmarshal(corpo, destino); err != nil {
			return 0, false, fmt.Errorf("tmdb: %s: JSON inválido: %w", operacao, catalogo.ErrTMDBIndisponivel)
		}
		return 0, false, nil
	case resp.StatusCode == http.StatusNotFound:
		c.registrar(ctx, operacao, "4xx", inicio)
		return 0, false, catalogo.ErrTMDBNaoEncontrado
	case resp.StatusCode == http.StatusTooManyRequests:
		c.registrar(ctx, operacao, "429", inicio)
		return retryAfter(resp.Header.Get("Retry-After")), true, fmt.Errorf("tmdb: %s: limite de requisições: %w", operacao, catalogo.ErrTMDBIndisponivel)
	case resp.StatusCode >= 500:
		c.registrar(ctx, operacao, "5xx", inicio)
		return 0, true, fmt.Errorf("tmdb: %s: status %d: %w", operacao, resp.StatusCode, catalogo.ErrTMDBIndisponivel)
	default:
		c.registrar(ctx, operacao, "4xx", inicio)
		return 0, false, fmt.Errorf("tmdb: %s: status %d: %w", operacao, resp.StatusCode, catalogo.ErrTMDBIndisponivel)
	}
}

func (c *Cliente) registrar(ctx context.Context, operacao, classe string, inicio time.Time) {
	c.chamadas.Add(ctx, 1, metric.WithAttributes(attribute.String("operacao", operacao), attribute.String("classe_status", classe)))
	c.duracao.Record(ctx, time.Since(inicio).Seconds(), metric.WithAttributes(attribute.String("operacao", operacao)))
}

// retryAfter lê segundos do header (teto de 5 s); inválido/ausente → 0.
func retryAfter(v string) time.Duration {
	s, err := strconv.Atoi(v)
	if err != nil || s <= 0 {
		return 0
	}
	d := time.Duration(s) * time.Second
	if d > tetoRetryAfter {
		return tetoRetryAfter
	}
	return d
}

// backoff exponencial (300 ms, 600 ms, ...) + até 20% de jitter.
func backoff(tentativa int) time.Duration {
	d := backoffBase << (tentativa - 1)
	if n, err := rand.Int(rand.Reader, big.NewInt(int64(d)/5+1)); err == nil {
		d += time.Duration(n.Int64())
	}
	return d
}

func dormirCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

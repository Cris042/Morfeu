// Package catalogo é o módulo dono dos filmes (doc.md §5): cartaz público,
// CRUD do operador no backoffice e o evento catalogo.filme_criado. Transaction
// script (ADR 0005). Não conhece autenticação: o backoffice recebe o middleware
// de papel já montado (PRD 0011 RNF01).
package catalogo

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/cache"
	"github.com/mclovin137/morfeu/internal/catalogo/db"
	"github.com/mclovin137/morfeu/internal/outbox"
)

const (
	// chaveCachePublico: cartaz público em cache read-through (PRD 0001 CA05,
	// renomeada na 0011); apagada de forma síncrona após toda escrita (RF05).
	chaveCachePublico = "catalogo:filmes:publicos"
	ttlCachePublico   = 5 * time.Minute

	// eventoFilmeCriado é o event_type emitido na criação (PRD 0002 RF01).
	eventoFilmeCriado = "catalogo.filme_criado"

	// Limites de entrada (RF04 do PRD 0011).
	tituloMax     = 255
	sinopseMax    = 2000
	duracaoMin    = 1
	duracaoMax    = 1440
	anoMin        = 1888
	anoMax        = 2100
	prefixoPoster = "https://image.tmdb.org/" // hotlink — decisão do usuário (refinamento E2)
)

var padraoImdb = regexp.MustCompile(`^tt[0-9]{7,10}$`)

// Filme é a visão do filme exposta pela API (JSON em PT).
type Filme struct {
	ID          int64      `json:"id"`
	Titulo      string     `json:"titulo"`
	Sinopse     *string    `json:"sinopse,omitempty"`
	DuracaoMin  *int32     `json:"duracao_min,omitempty"`
	Ano         *int32     `json:"ano,omitempty"`
	PosterURL   *string    `json:"poster_url,omitempty"`
	ImdbID      *string    `json:"imdb_id,omitempty"`
	TmdbID      *int64     `json:"tmdb_id,omitempty"`
	ArquivadoEm *time.Time `json:"arquivado_em,omitempty"`
}

// DadosFilme são os campos editáveis (criação manual, edição e CLI).
type DadosFilme struct {
	Titulo     string
	Sinopse    *string
	DuracaoMin *int32
	Ano        *int32
	PosterURL  *string
	ImdbID     *string
}

// filmeCriadoPayload é o payload JSON de catalogo.filme_criado — em PT desde
// a 0011 (sem versionamento: único consumidor é a projeção interna).
type filmeCriadoPayload struct {
	ID         int64   `json:"id"`
	Titulo     string  `json:"titulo"`
	Ano        *int32  `json:"ano,omitempty"`
	DuracaoMin *int32  `json:"duracao_min,omitempty"`
	Sinopse    *string `json:"sinopse,omitempty"`
}

// Servico implementa os casos de uso do catálogo.
type Servico struct {
	q      *db.Queries
	pool   outbox.Pool
	cache  cache.Cache // nil na CLI (sem cartaz a invalidar/servir)
	logger *zap.Logger
	tmdb   FonteTMDB // opcional (PRD 0012): nil sem TMDB_API_TOKEN
}

// NovoServico cria o serviço. pool abre as transações via outbox.WithTx
// (ADR 0002) — o domínio nunca importa o driver.
func NovoServico(q *db.Queries, pool outbox.Pool, c cache.Cache, logger *zap.Logger) *Servico {
	return &Servico{q: q, pool: pool, cache: c, logger: logger}
}

// ListarPublicos devolve o cartaz (não arquivados) com cache read-through.
func (s *Servico) ListarPublicos(ctx context.Context) ([]Filme, error) {
	if filmes, ok := s.lerCache(ctx); ok {
		return filmes, nil
	}
	linhas, err := s.q.ListarFilmesPublicos(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalogo: listar cartaz: %w", err)
	}
	filmes := make([]Filme, len(linhas))
	for i, l := range linhas {
		filmes[i] = Filme{ID: l.ID, Titulo: l.Titulo, Sinopse: l.Sinopse, DuracaoMin: l.DuracaoMin, Ano: l.Ano,
			PosterURL: l.PosterUrl, ImdbID: l.ImdbID, TmdbID: l.TmdbID}
	}
	s.gravarCache(ctx, filmes)
	return filmes, nil
}

// BuscarPublico devolve um filme não arquivado.
func (s *Servico) BuscarPublico(ctx context.Context, id int64) (Filme, error) {
	linhas, err := s.q.BuscarFilmePublico(ctx, id)
	if err != nil {
		return Filme{}, fmt.Errorf("catalogo: buscar filme: %w", err)
	}
	if len(linhas) == 0 {
		return Filme{}, ErrFilmeNaoEncontrado
	}
	l := linhas[0]
	return Filme{ID: l.ID, Titulo: l.Titulo, Sinopse: l.Sinopse, DuracaoMin: l.DuracaoMin, Ano: l.Ano,
		PosterURL: l.PosterUrl, ImdbID: l.ImdbID, TmdbID: l.TmdbID}, nil
}

// ListarBackoffice devolve todos os filmes, inclusive arquivados.
func (s *Servico) ListarBackoffice(ctx context.Context) ([]Filme, error) {
	linhas, err := s.q.ListarFilmesBackoffice(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalogo: listar backoffice: %w", err)
	}
	filmes := make([]Filme, len(linhas))
	for i, l := range linhas {
		filmes[i] = Filme{ID: l.ID, Titulo: l.Titulo, Sinopse: l.Sinopse, DuracaoMin: l.DuracaoMin, Ano: l.Ano,
			PosterURL: l.PosterUrl, ImdbID: l.ImdbID, TmdbID: l.TmdbID, ArquivadoEm: l.ArquivadoEm}
	}
	return filmes, nil
}

// DuracaoFilmeAtivo implementa a porta sessao.FonteFilmes (PRD 0013 RF06):
// o módulo sessao pergunta a duração de um filme sem ler a tabela filmes.
// ok=false quando o filme não existe, está arquivado ou não tem duração.
func (s *Servico) DuracaoFilmeAtivo(ctx context.Context, filmeID int64) (int32, bool, error) {
	linhas, err := s.q.BuscarDuracaoFilme(ctx, filmeID)
	if err != nil {
		return 0, false, fmt.Errorf("catalogo: duração do filme: %w", err)
	}
	if len(linhas) == 0 || linhas[0].ArquivadoEm != nil || linhas[0].DuracaoMin == nil || *linhas[0].DuracaoMin <= 0 {
		return 0, false, nil
	}
	return *linhas[0].DuracaoMin, true, nil
}

// TituloDoFilme é a porta da notificação (PRD 0028): o título de um filme,
// mesmo arquivado. ok=false quando o filme não existe.
func (s *Servico) TituloDoFilme(ctx context.Context, filmeID int64) (string, bool, error) {
	linhas, err := s.q.BuscarTituloFilme(ctx, filmeID)
	if err != nil {
		return "", false, fmt.Errorf("catalogo: título do filme: %w", err)
	}
	if len(linhas) == 0 {
		return "", false, nil
	}
	return linhas[0], true, nil
}

// Criar insere o filme e enfileira catalogo.filme_criado na MESMA TX (PRD
// 0002 RN01) e invalida o cartaz após o commit (RF05).
func (s *Servico) Criar(ctx context.Context, dados DadosFilme) (Filme, error) {
	d, err := validar(dados)
	if err != nil {
		return Filme{}, err
	}
	var criado Filme
	err = outbox.WithTx(ctx, s.pool, func(tx outbox.Tx) error {
		l, err := s.q.WithTx(tx).InserirFilme(ctx, db.InserirFilmeParams{
			Titulo: d.Titulo, Sinopse: d.Sinopse, DuracaoMin: d.DuracaoMin, Ano: d.Ano, PosterUrl: d.PosterURL, ImdbID: d.ImdbID,
		})
		if err != nil {
			return fmt.Errorf("catalogo: inserir filme: %w", err)
		}
		payload, err := json.Marshal(filmeCriadoPayload{ID: l.ID, Titulo: l.Titulo, Ano: l.Ano, DuracaoMin: l.DuracaoMin, Sinopse: l.Sinopse})
		if err != nil {
			return fmt.Errorf("catalogo: serializar %s: %w", eventoFilmeCriado, err)
		}
		if _, err := outbox.Enqueue(ctx, tx, outbox.Evento{
			EventType: eventoFilmeCriado, AggregateID: strconv.FormatInt(l.ID, 10), OccurredAt: time.Now(), Payload: payload,
		}); err != nil {
			return err
		}
		criado = Filme{ID: l.ID, Titulo: l.Titulo, Sinopse: l.Sinopse, DuracaoMin: l.DuracaoMin, Ano: l.Ano,
			PosterURL: l.PosterUrl, ImdbID: l.ImdbID, TmdbID: l.TmdbID}
		return nil
	})
	if err != nil {
		return Filme{}, err
	}
	s.invalidarCartaz(ctx)
	s.logger.Info("filme criado", zap.Int64("filme_id", criado.ID), zap.String("event_type", eventoFilmeCriado))
	return criado, nil
}

// Atualizar substitui os campos editáveis (RF03).
func (s *Servico) Atualizar(ctx context.Context, id int64, dados DadosFilme) (Filme, error) {
	d, err := validar(dados)
	if err != nil {
		return Filme{}, err
	}
	linhas, err := s.q.AtualizarFilme(ctx, db.AtualizarFilmeParams{
		ID: id, Titulo: d.Titulo, Sinopse: d.Sinopse, DuracaoMin: d.DuracaoMin, Ano: d.Ano, PosterUrl: d.PosterURL, ImdbID: d.ImdbID,
	})
	if err != nil {
		return Filme{}, fmt.Errorf("catalogo: atualizar filme: %w", err)
	}
	if len(linhas) == 0 {
		return Filme{}, ErrFilmeNaoEncontrado
	}
	s.invalidarCartaz(ctx)
	l := linhas[0]
	s.logger.Info("filme atualizado", zap.Int64("filme_id", l.ID))
	return Filme{ID: l.ID, Titulo: l.Titulo, Sinopse: l.Sinopse, DuracaoMin: l.DuracaoMin, Ano: l.Ano,
		PosterURL: l.PosterUrl, ImdbID: l.ImdbID, TmdbID: l.TmdbID}, nil
}

// Arquivar tira o filme do cartaz sem apagá-lo (RN01). Idempotente.
func (s *Servico) Arquivar(ctx context.Context, id int64) error {
	n, err := s.q.ArquivarFilme(ctx, id)
	if err != nil {
		return fmt.Errorf("catalogo: arquivar filme: %w", err)
	}
	if n == 0 {
		return ErrFilmeNaoEncontrado
	}
	s.invalidarCartaz(ctx)
	s.logger.Info("filme arquivado", zap.Int64("filme_id", id))
	return nil
}

func (s *Servico) lerCache(ctx context.Context) ([]Filme, bool) {
	if s.cache == nil {
		return nil, false
	}
	dados, err := s.cache.Get(ctx, chaveCachePublico)
	if err != nil || dados == nil {
		return nil, false
	}
	var filmes []Filme
	if err := json.Unmarshal(dados, &filmes); err != nil {
		s.logger.Warn("cartaz em cache ilegível — lendo do banco", zap.Error(err))
		return nil, false
	}
	return filmes, true
}

func (s *Servico) gravarCache(ctx context.Context, filmes []Filme) {
	if s.cache == nil {
		return
	}
	if dados, err := json.Marshal(filmes); err == nil {
		_ = s.cache.Set(ctx, chaveCachePublico, dados, ttlCachePublico) // best effort; RedisCache já loga
	}
}

// invalidarCartaz apaga o cartaz em cache depois do commit (RF05). Falha =
// warn: a inconsistência fica limitada pelo TTL.
func (s *Servico) invalidarCartaz(ctx context.Context) {
	if s.cache == nil {
		return
	}
	if err := s.cache.Delete(ctx, chaveCachePublico); err != nil {
		s.logger.Warn("não foi possível invalidar o cartaz em cache (TTL limita a defasagem)", zap.Error(err))
	}
}

// validar normaliza e aplica as regras do RF04; devolve os campos inválidos
// sem ecoar valores.
func validar(d DadosFilme) (DadosFilme, error) {
	d.Titulo = strings.TrimSpace(d.Titulo)
	d.Sinopse = aparar(d.Sinopse)
	d.PosterURL = aparar(d.PosterURL)
	d.ImdbID = aparar(d.ImdbID)

	var campos []string
	if n := utf8.RuneCountInString(d.Titulo); n == 0 || n > tituloMax {
		campos = append(campos, "titulo")
	}
	if d.Sinopse != nil && utf8.RuneCountInString(*d.Sinopse) > sinopseMax {
		campos = append(campos, "sinopse")
	}
	if d.DuracaoMin == nil || *d.DuracaoMin < duracaoMin || *d.DuracaoMin > duracaoMax {
		campos = append(campos, "duracao_min")
	}
	if d.Ano != nil && (*d.Ano < anoMin || *d.Ano > anoMax) {
		campos = append(campos, "ano")
	}
	if d.PosterURL != nil && (!strings.HasPrefix(*d.PosterURL, prefixoPoster) || len(*d.PosterURL) > 500) {
		campos = append(campos, "poster_url")
	}
	if d.ImdbID != nil && !padraoImdb.MatchString(*d.ImdbID) {
		campos = append(campos, "imdb_id")
	}
	if len(campos) > 0 {
		return DadosFilme{}, &ErroValidacao{Campos: campos}
	}
	return d, nil
}

// aparar faz trim e trata string vazia como ausente.
func aparar(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return &t
}

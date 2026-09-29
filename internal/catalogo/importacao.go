package catalogo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/catalogo/db"
	"github.com/mclovin137/morfeu/internal/outbox"
)

// AtribuicaoTMDB é o aviso exigido pelos termos de uso da API do TMDB
// (doc.md §12) — devolvido em toda resposta que carrega dados do TMDB.
const AtribuicaoTMDB = "Este produto usa a API do TMDB, mas não é endossado nem certificado pelo TMDB."

// basePoster: hotlink da CDN do TMDB (decisão do usuário no refinamento E2).
// A URL é MONTADA aqui a partir do poster_path validado — nunca copiada da
// resposta externa.
const basePoster = "https://image.tmdb.org/t/p/w500"

// Erros da integração com o TMDB (PRD 0012 RF01/RF04).
var (
	ErrTMDBNaoConfigurado = errors.New("catalogo: TMDB não configurado")
	ErrTMDBNaoEncontrado  = errors.New("catalogo: filme não encontrado no TMDB")
	ErrTMDBIndisponivel   = errors.New("catalogo: TMDB indisponível")
	ErrTMDBSemDuracao     = errors.New("catalogo: filme do TMDB sem duração")
)

// DadosTMDB é o detalhe de um filme como o adapter o entrega (dados externos,
// ainda NÃO confiáveis — normalizados em paraDadosFilme).
type DadosTMDB struct {
	TmdbID     int64
	Titulo     string
	Sinopse    string
	DuracaoMin int
	DataLanc   string // "AAAA-MM-DD" ou vazio
	PosterPath string
	ImdbID     string
}

// ResultadoTMDB é um item da busca.
type ResultadoTMDB struct {
	TmdbID    int64   `json:"tmdb_id"`
	Titulo    string  `json:"titulo"`
	Ano       *int32  `json:"ano,omitempty"`
	PosterURL *string `json:"poster_url,omitempty"`
}

// NovoResultadoTMDB é usado pelo adapter; campos derivados são calculados aqui
// (mesma normalização do detalhe).
func NovoResultadoTMDB(tmdbID int64, titulo, dataLanc, posterPath string) ResultadoTMDB {
	r := ResultadoTMDB{TmdbID: tmdbID, Titulo: truncar(limparTexto(titulo), tituloMax)}
	r.Ano = anoDe(dataLanc)
	r.PosterURL = posterDe(posterPath)
	return r
}

// FonteTMDB é o port do catálogo para o TMDB (ADR 0003: interface no
// consumidor; implementações: adapter HTTP e o fake httptest dos testes).
type FonteTMDB interface {
	Detalhar(ctx context.Context, tmdbID int64) (DadosTMDB, error)
	Buscar(ctx context.Context, termo string) ([]ResultadoTMDB, error)
}

// ComFonteTMDB liga o serviço ao TMDB (opcional: sem token, as rotas de TMDB
// respondem 503 e o cadastro manual segue funcionando).
func (s *Servico) ComFonteTMDB(f FonteTMDB) *Servico {
	s.tmdb = f
	return s
}

// BuscarNoTMDB pesquisa filmes para o operador escolher (RF02).
func (s *Servico) BuscarNoTMDB(ctx context.Context, termo string) ([]ResultadoTMDB, error) {
	termo = strings.TrimSpace(termo)
	if n := utf8.RuneCountInString(termo); n < 2 || n > 100 {
		return nil, &ErroValidacao{Campos: []string{"q"}}
	}
	if s.tmdb == nil {
		return nil, ErrTMDBNaoConfigurado
	}
	return s.tmdb.Buscar(ctx, termo)
}

// ImportarDoTMDB traz o filme do TMDB para o catálogo (RF03): upsert por
// tmdb_id numa TX; evento catalogo.filme_criado só na criação; cartaz
// invalidado após o commit. criado=false numa reimportação.
func (s *Servico) ImportarDoTMDB(ctx context.Context, tmdbID int64) (Filme, bool, error) {
	if tmdbID <= 0 {
		return Filme{}, false, &ErroValidacao{Campos: []string{"tmdb_id"}}
	}
	if s.tmdb == nil {
		return Filme{}, false, ErrTMDBNaoConfigurado
	}
	externo, err := s.tmdb.Detalhar(ctx, tmdbID)
	if err != nil {
		return Filme{}, false, err
	}
	if externo.DuracaoMin <= 0 {
		return Filme{}, false, ErrTMDBSemDuracao
	}
	dados, err := validar(paraDadosFilme(externo))
	if err != nil {
		return Filme{}, false, fmt.Errorf("catalogo: dados do TMDB fora das regras: %w", err)
	}

	var (
		filme  Filme
		criado bool
	)
	err = outbox.WithTx(ctx, s.pool, func(tx outbox.Tx) error {
		l, err := s.q.WithTx(tx).UpsertFilmeTMDB(ctx, db.UpsertFilmeTMDBParams{
			TmdbID: &tmdbID, Titulo: dados.Titulo, Sinopse: dados.Sinopse, DuracaoMin: dados.DuracaoMin,
			Ano: dados.Ano, PosterUrl: dados.PosterURL, ImdbID: dados.ImdbID,
		})
		if err != nil {
			return fmt.Errorf("catalogo: upsert do filme do TMDB: %w", err)
		}
		filme = Filme{ID: l.ID, Titulo: l.Titulo, Sinopse: l.Sinopse, DuracaoMin: l.DuracaoMin, Ano: l.Ano,
			PosterURL: l.PosterUrl, ImdbID: l.ImdbID, TmdbID: l.TmdbID}
		criado = l.Inserido
		if !criado {
			return nil
		}
		payload, err := json.Marshal(filmeCriadoPayload{ID: l.ID, Titulo: l.Titulo, Ano: l.Ano, DuracaoMin: l.DuracaoMin, Sinopse: l.Sinopse})
		if err != nil {
			return fmt.Errorf("catalogo: serializar %s: %w", eventoFilmeCriado, err)
		}
		_, err = outbox.Enqueue(ctx, tx, outbox.Evento{
			EventType: eventoFilmeCriado, AggregateID: strconv.FormatInt(l.ID, 10), OccurredAt: time.Now(), Payload: payload,
		})
		return err
	})
	if err != nil {
		return Filme{}, false, err
	}
	s.invalidarCartaz(ctx)
	s.logger.Info("filme importado do TMDB", zap.Int64("filme_id", filme.ID), zap.Int64("tmdb_id", tmdbID), zap.Bool("criado", criado))
	return filme, criado, nil
}

var (
	padraoTag        = regexp.MustCompile(`<[^>]*>`)
	padraoPosterPath = regexp.MustCompile(`^/[A-Za-z0-9_\-]{1,100}\.(jpg|jpeg|png|webp)$`)
	padraoData       = regexp.MustCompile(`^([0-9]{4})-[0-9]{2}-[0-9]{2}$`)
)

// paraDadosFilme normaliza os dados externos: sem HTML, dentro dos limites,
// pôster só por path no formato esperado, imdb só se válido (RF03).
func paraDadosFilme(e DadosTMDB) DadosFilme {
	d := DadosFilme{Titulo: truncar(limparTexto(e.Titulo), tituloMax)}
	if sin := truncar(limparTexto(e.Sinopse), sinopseMax); sin != "" {
		d.Sinopse = &sin
	}
	if e.DuracaoMin > 0 && e.DuracaoMin <= duracaoMax {
		dur := int32(e.DuracaoMin) //nolint:gosec // G115: faixa 1..1440 checada acima
		d.DuracaoMin = &dur
	}
	d.Ano = anoDe(e.DataLanc)
	d.PosterURL = posterDe(e.PosterPath)
	if imdb := strings.TrimSpace(e.ImdbID); padraoImdb.MatchString(imdb) {
		d.ImdbID = &imdb
	}
	return d
}

func limparTexto(s string) string {
	return strings.TrimSpace(padraoTag.ReplaceAllString(s, ""))
}

func truncar(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

func anoDe(data string) *int32 {
	m := padraoData.FindStringSubmatch(data)
	if m == nil {
		return nil
	}
	ano, err := strconv.ParseInt(m[1], 10, 32)
	if err != nil || ano < anoMin || ano > anoMax {
		return nil
	}
	a := int32(ano)
	return &a
}

func posterDe(path string) *string {
	if !padraoPosterPath.MatchString(path) {
		return nil
	}
	u := basePoster + path
	return &u
}

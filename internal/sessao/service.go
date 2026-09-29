// Package sessao é o módulo dono das salas e da programação de sessões
// (doc.md §5/§6.3): layout das salas, sessões com snapshot da duração do
// filme e a regra de não-conflito garantida pelo banco (EXCLUDE). Transaction
// script (ADR 0005). Não lê filmes (a duração vem pela porta FonteFilmes) nem
// conhece autenticação ou OTel — tudo isso é injetado pelo main (ADR 0003).
package sessao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/sessao/db"
)

// IntervaloLimpeza entre sessões na mesma sala (decisão do usuário no
// refinamento E3: 20 min). Entra no fim de cada sessão.
const IntervaloLimpeza = 20 * time.Minute

// Faixas de entrada (RF04 do PRD 0013; espelham os CHECKs da migration 008).
const (
	precoMin    = 100
	precoMax    = 100000
	nomeSalaMax = 80
)

// FonteFilmes é a porta para o catálogo (RF06): o sessao pergunta a duração
// de um filme ATIVO sem ler a tabela filmes. ok=false quando o filme não
// existe, está arquivado ou não tem duração.
type FonteFilmes interface {
	DuracaoFilmeAtivo(ctx context.Context, filmeID int64) (duracaoMin int32, ok bool, err error)
}

// Config agrupa as dependências injetadas pelo main.
type Config struct {
	Filmes     FonteFilmes
	AoConflito func(context.Context) // incrementa sessao_conflitos_total (RF07)
	Agora      func() time.Time
}

// Servico implementa os casos de uso do módulo.
type Servico struct {
	q      *db.Queries
	cfg    Config
	logger *zap.Logger
}

// NovoServico cria o serviço; Filmes é obrigatório.
func NovoServico(q *db.Queries, cfg Config, logger *zap.Logger) (*Servico, error) {
	if cfg.Filmes == nil {
		return nil, errors.New("sessao: porta de filmes ausente")
	}
	if cfg.AoConflito == nil {
		cfg.AoConflito = func(context.Context) {}
	}
	if cfg.Agora == nil {
		cfg.Agora = time.Now
	}
	return &Servico{q: q, cfg: cfg, logger: logger}, nil
}

// Sala é a visão da sala na API.
type Sala struct {
	ID     int64  `json:"id"`
	Nome   string `json:"nome"`
	Layout Layout `json:"layout"`
}

// Sessao é a visão da sessão na API (horários em UTC; exibição no fuso do
// cinema fica para a borda — E5).
type Sessao struct {
	ID            int64     `json:"id"`
	FilmeID       int64     `json:"filme_id"`
	SalaID        int64     `json:"sala_id"`
	Inicio        time.Time `json:"inicio"`
	Fim           time.Time `json:"fim"`
	DuracaoMin    int32     `json:"duracao_min"`
	PrecoCentavos int32     `json:"preco_centavos"`
	Status        string    `json:"status"`
}

// EntradaSessao são os dados da criação de sessão.
type EntradaSessao struct {
	FilmeID       int64
	SalaID        int64
	Inicio        time.Time
	PrecoCentavos int32
}

// CalcularFim aplica RN01: fim = início + duração do filme + limpeza.
func CalcularFim(inicio time.Time, duracaoMin int32) time.Time {
	return inicio.Add(time.Duration(duracaoMin)*time.Minute + IntervaloLimpeza)
}

// CriarSala cadastra uma sala com layout validado (RF03).
func (s *Servico) CriarSala(ctx context.Context, nome string, layoutJSON []byte, operador string) (Sala, error) {
	nome, layout, err := validarSala(nome, layoutJSON)
	if err != nil {
		return Sala{}, err
	}
	bruto, _ := json.Marshal(layout)
	l, err := s.q.InserirSala(ctx, db.InserirSalaParams{Nome: nome, Layout: bruto})
	if err != nil {
		if db.EhNomeDuplicado(err) {
			return Sala{}, ErrNomeSalaEmUso
		}
		return Sala{}, fmt.Errorf("sessao: inserir sala: %w", err)
	}
	s.logger.Info("sala criada", zap.String("operador_id", operador), zap.Int64("sala_id", l.ID))
	return Sala{ID: l.ID, Nome: l.Nome, Layout: layout}, nil
}

// AtualizarSala troca nome e layout; o layout é imutável enquanto houver
// sessão agendada futura na sala (RN03).
func (s *Servico) AtualizarSala(ctx context.Context, id int64, nome string, layoutJSON []byte, operador string) (Sala, error) {
	nome, layout, err := validarSala(nome, layoutJSON)
	if err != nil {
		return Sala{}, err
	}
	atuais, err := s.q.BuscarSala(ctx, id)
	if err != nil {
		return Sala{}, fmt.Errorf("sessao: buscar sala: %w", err)
	}
	if len(atuais) == 0 {
		return Sala{}, ErrSalaNaoEncontrada
	}
	var layoutAtual Layout
	_ = json.Unmarshal(atuais[0].Layout, &layoutAtual)
	bruto, _ := json.Marshal(layout)
	if !mesmoLayout(layoutAtual, layout) {
		emUso, err := s.q.ExisteSessaoFuturaNaSala(ctx, db.ExisteSessaoFuturaNaSalaParams{SalaID: id, Fim: s.cfg.Agora()})
		if err != nil {
			return Sala{}, fmt.Errorf("sessao: checar sessões da sala: %w", err)
		}
		if emUso {
			return Sala{}, ErrLayoutEmUso
		}
	}
	linhas, err := s.q.AtualizarSala(ctx, db.AtualizarSalaParams{ID: id, Nome: nome, Layout: bruto})
	if err != nil {
		if db.EhNomeDuplicado(err) {
			return Sala{}, ErrNomeSalaEmUso
		}
		return Sala{}, fmt.Errorf("sessao: atualizar sala: %w", err)
	}
	if len(linhas) == 0 {
		return Sala{}, ErrSalaNaoEncontrada
	}
	s.logger.Info("sala atualizada", zap.String("operador_id", operador), zap.Int64("sala_id", id))
	return Sala{ID: id, Nome: nome, Layout: layout}, nil
}

// ListarSalas devolve as salas com o layout.
func (s *Servico) ListarSalas(ctx context.Context) ([]Sala, error) {
	linhas, err := s.q.ListarSalas(ctx)
	if err != nil {
		return nil, fmt.Errorf("sessao: listar salas: %w", err)
	}
	out := make([]Sala, len(linhas))
	for i, l := range linhas {
		out[i] = Sala{ID: l.ID, Nome: l.Nome}
		_ = json.Unmarshal(l.Layout, &out[i].Layout)
	}
	return out, nil
}

// CriarSessao programa uma sessão (RF04): valida entrada, consulta a
// duração pela porta (snapshot), calcula o fim e deixa o BANCO decidir o
// conflito (EXCLUDE) — sem janela de corrida entre checar e inserir.
func (s *Servico) CriarSessao(ctx context.Context, in EntradaSessao, operador string) (Sessao, error) {
	var campos []string
	if !in.Inicio.After(s.cfg.Agora()) {
		campos = append(campos, "inicio")
	}
	if in.PrecoCentavos < precoMin || in.PrecoCentavos > precoMax {
		campos = append(campos, "preco_centavos")
	}
	if len(campos) > 0 {
		return Sessao{}, &ErroValidacao{Campos: campos}
	}
	salas, err := s.q.BuscarSala(ctx, in.SalaID)
	if err != nil {
		return Sessao{}, fmt.Errorf("sessao: buscar sala: %w", err)
	}
	if len(salas) == 0 {
		return Sessao{}, ErrSalaInexistente
	}
	duracao, ok, err := s.cfg.Filmes.DuracaoFilmeAtivo(ctx, in.FilmeID)
	if err != nil {
		return Sessao{}, fmt.Errorf("sessao: consultar filme: %w", err)
	}
	if !ok {
		return Sessao{}, ErrFilmeIndisponivel
	}
	inicio := in.Inicio.UTC()
	fim := CalcularFim(inicio, duracao)

	l, err := s.inserirSessao(ctx, db.InserirSessaoParams{
		FilmeID: in.FilmeID, SalaID: in.SalaID, Inicio: inicio, DuracaoMin: duracao, Fim: fim, PrecoCentavos: in.PrecoCentavos,
	})
	if err != nil {
		if db.EhConflitoDeHorario(err) {
			s.cfg.AoConflito(ctx)
			return Sessao{}, s.conflito(ctx, in.SalaID, inicio, fim)
		}
		return Sessao{}, fmt.Errorf("sessao: inserir sessão (sqlstate %s): %w", db.CodigoSQL(err), err)
	}
	s.logger.Info("sessão criada", zap.String("operador_id", operador), zap.Int64("sessao_id", l.ID))
	return Sessao{ID: l.ID, FilmeID: l.FilmeID, SalaID: l.SalaID, Inicio: l.Inicio, Fim: l.Fim,
		DuracaoMin: l.DuracaoMin, PrecoCentavos: l.PrecoCentavos, Status: l.Status}, nil
}

// maxTentativasInsercao: deadlocks da EXCLUDE sob concorrência são raros e
// se resolvem na tentativa seguinte (a concorrente já commitou).
const maxTentativasInsercao = 3

// inserirSessao repete o INSERT só em deadlock (40P01); qualquer outro erro
// (inclusive o 23P01 de conflito) volta na hora.
func (s *Servico) inserirSessao(ctx context.Context, p db.InserirSessaoParams) (db.InserirSessaoRow, error) {
	var (
		l   db.InserirSessaoRow
		err error
	)
	for tentativa := 1; tentativa <= maxTentativasInsercao; tentativa++ {
		l, err = s.q.InserirSessao(ctx, p)
		if err == nil || !db.EhImpasse(err) {
			return l, err
		}
		s.logger.Warn("impasse na EXCLUDE de sessões — repetindo", zap.Int("tentativa", tentativa))
	}
	return l, err
}

// conflito reconsulta a sessão que ocupa o intervalo para o 409.
func (s *Servico) conflito(ctx context.Context, salaID int64, inicio, fim time.Time) error {
	linhas, err := s.q.SessaoConflitante(ctx, db.SessaoConflitanteParams{SalaID: salaID, Inicio: inicio, Fim: fim})
	if err != nil || len(linhas) == 0 {
		// A conflitante pode ter sido cancelada entre o erro e a consulta:
		// ainda é conflito do ponto de vista desta tentativa.
		return &ErroConflitoHorario{}
	}
	return &ErroConflitoHorario{SessaoID: linhas[0].ID, Inicio: linhas[0].Inicio, Fim: linhas[0].Fim}
}

// ListarSessoesBackoffice devolve as sessões mais recentes (RF05).
func (s *Servico) ListarSessoesBackoffice(ctx context.Context) ([]Sessao, error) {
	linhas, err := s.q.ListarSessoesBackoffice(ctx)
	if err != nil {
		return nil, fmt.Errorf("sessao: listar sessões: %w", err)
	}
	out := make([]Sessao, len(linhas))
	for i, l := range linhas {
		out[i] = Sessao{ID: l.ID, FilmeID: l.FilmeID, SalaID: l.SalaID, Inicio: l.Inicio, Fim: l.Fim,
			DuracaoMin: l.DuracaoMin, PrecoCentavos: l.PrecoCentavos, Status: l.Status}
	}
	return out, nil
}

// CancelarSessao tira a sessão da programação (idempotente) e libera o
// horário na EXCLUDE. Cancelar com ingressos vendidos é E9.
func (s *Servico) CancelarSessao(ctx context.Context, id int64, operador string) error {
	n, err := s.q.CancelarSessao(ctx, id)
	if err != nil {
		return fmt.Errorf("sessao: cancelar sessão: %w", err)
	}
	if n == 0 {
		return ErrSessaoNaoEncontrada
	}
	s.logger.Info("sessão cancelada", zap.String("operador_id", operador), zap.Int64("sessao_id", id))
	return nil
}

func validarSala(nome string, layoutJSON []byte) (string, Layout, error) {
	nome = strings.TrimSpace(nome)
	layout, err := LerLayout(layoutJSON)
	var ev *ErroValidacao
	var campos []string
	if n := utf8.RuneCountInString(nome); n == 0 || n > nomeSalaMax {
		campos = append(campos, "nome")
	}
	if errors.As(err, &ev) {
		campos = append(campos, ev.Campos...)
	}
	if len(campos) > 0 {
		return "", Layout{}, &ErroValidacao{Campos: campos}
	}
	return nome, layout, nil
}

func mesmoLayout(a, b Layout) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

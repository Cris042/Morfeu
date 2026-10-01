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
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/auditoria"
	"github.com/mclovin137/morfeu/internal/cache"
	"github.com/mclovin137/morfeu/internal/outbox"
	"github.com/mclovin137/morfeu/internal/sessao/db"
)

// ttlCachePublico: defasagem máxima das sessões públicas de um filme (PRD
// 0014 RF02); escrita invalida na hora, o TTL só cobre falha do delete.
const ttlCachePublico = 60 * time.Second

func chaveCacheFilme(filmeID int64) string {
	return fmt.Sprintf("sessao:filme:%d:futuras", filmeID)
}

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

// PedidosDaSessao é a porta transacional para o módulo pedido (ADR 0011): na
// TX do cancelamento da sessão, manda para estorno os pedidos pagos dela e
// devolve quantos foram. O adapter é ligado no main (nenhum módulo importa o
// outro).
type PedidosDaSessao interface {
	CancelarPedidosDaSessao(ctx context.Context, tx outbox.Tx, sessaoID int64) (int64, error)
}

// Config agrupa as dependências injetadas pelo main.
type Config struct {
	Filmes FonteFilmes
	// Pool abre a TX das mutações (trilha de auditoria na mesma TX — PRD
	// 0037; cancelamento da sessão — PRD 0036); nil só em testes sem mutação.
	Pool       outbox.Pool
	AoConflito func(context.Context) // incrementa sessao_conflitos_total (RF07)
	Agora      func() time.Time
	Cache      cache.Cache // opcional (PRD 0014): cache das sessões públicas
}

// Servico implementa os casos de uso do módulo.
type Servico struct {
	q      *db.Queries
	cfg    Config
	logger *zap.Logger
	// pedidos e aoEstornar são ligados depois de criar o pedido (que depende
	// deste serviço) — ver LigarPedidos.
	pedidos    PedidosDaSessao
	aoEstornar func(ctx context.Context, n int64)
}

// LigarPedidos liga a porta do pedido (ADR 0011). aoEstornar roda depois do
// commit com o número de pedidos mandados para estorno (métrica).
func (s *Servico) LigarPedidos(p PedidosDaSessao, aoEstornar func(ctx context.Context, n int64)) {
	s.pedidos, s.aoEstornar = p, aoEstornar
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
	var l db.InserirSalaRow
	err = s.naTx(ctx, func(q *db.Queries, tx outbox.Tx) error {
		var err error
		if l, err = q.InserirSala(ctx, db.InserirSalaParams{Nome: nome, Layout: bruto}); err != nil {
			return err
		}
		return auditoria.Registrar(ctx, tx, auditoria.SalaCriada, auditoria.ID(l.ID), s.cfg.Agora())
	})
	if err != nil {
		return Sala{}, erroDaSala(err, "inserir")
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
	err = s.naTx(ctx, func(q *db.Queries, tx outbox.Tx) error {
		linhas, err := q.AtualizarSala(ctx, db.AtualizarSalaParams{ID: id, Nome: nome, Layout: bruto})
		if err != nil {
			return err
		}
		if len(linhas) == 0 {
			return ErrSalaNaoEncontrada
		}
		return auditoria.Registrar(ctx, tx, auditoria.SalaAtualizada, auditoria.ID(id), s.cfg.Agora())
	})
	if err != nil {
		return Sala{}, erroDaSala(err, "atualizar")
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
	s.invalidarFilme(ctx, l.FilmeID)
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
		err = s.naTx(ctx, func(q *db.Queries, tx outbox.Tx) error {
			var err error
			if l, err = q.InserirSessao(ctx, p); err != nil {
				return err
			}
			return auditoria.Registrar(ctx, tx, auditoria.SessaoCriada, auditoria.ID(l.ID), s.cfg.Agora())
		})
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

// FiltroSessoes restringe a listagem do operador (PRD 0014 RF05).
type FiltroSessoes struct {
	SalaID *int64
	Dia    *time.Time // dia em UTC (00:00 até 24:00)
}

// ListarSessoesBackoffice devolve as sessões mais recentes, com filtros.
func (s *Servico) ListarSessoesBackoffice(ctx context.Context, f FiltroSessoes) ([]Sessao, error) {
	p := db.ListarSessoesBackofficeParams{SalaID: f.SalaID}
	if f.Dia != nil {
		desde := time.Date(f.Dia.Year(), f.Dia.Month(), f.Dia.Day(), 0, 0, 0, 0, time.UTC)
		ate := desde.AddDate(0, 0, 1)
		p.Desde, p.Ate = &desde, &ate
	}
	linhas, err := s.q.ListarSessoesBackoffice(ctx, p)
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

// CancelarSessao tira a sessão da programação (idempotente), libera o
// horário na EXCLUDE e invalida a lista pública do filme. Numa TX única, os
// pedidos pagos vão para estorno pela porta do pedido (ADR 0011); sessão
// agendada que já começou não é cancelada. Devolve quantos pedidos foram.
func (s *Servico) CancelarSessao(ctx context.Context, id int64, operador string) (int64, error) {
	if s.cfg.Pool == nil || s.pedidos == nil {
		// Sem a porta, pedidos pagos ficariam sem estorno (auditoria 0036).
		return 0, errors.New("sessao: cancelar sem pool ou sem a porta do pedido")
	}
	var filmeID, n int64
	err := outbox.WithTx(ctx, s.cfg.Pool, func(tx outbox.Tx) error {
		var err error
		filmeID, n, err = s.cancelarNaTx(ctx, tx, id)
		return err
	})
	if err != nil {
		return 0, err
	}
	if s.aoEstornar != nil {
		s.aoEstornar(ctx, n)
	}
	s.invalidarFilme(ctx, filmeID)
	s.logger.Info("sessão cancelada", zap.String("operador_id", operador), zap.Int64("sessao_id", id), zap.Int64("pedidos_estornados", n))
	return n, nil
}

func (s *Servico) cancelarNaTx(ctx context.Context, tx outbox.Tx, id int64) (filmeID, n int64, err error) {
	q := s.q.WithTx(tx)
	linhas, err := q.TravarSessao(ctx, id)
	if err != nil {
		return 0, 0, fmt.Errorf("sessao: travar sessão: %w", err)
	}
	if len(linhas) == 0 {
		return 0, 0, ErrSessaoNaoEncontrada
	}
	if linhas[0].Status == statusAgendada && !linhas[0].Inicio.After(s.cfg.Agora()) {
		return 0, 0, ErrSessaoIniciada
	}
	if _, err := q.CancelarSessao(ctx, id); err != nil {
		return 0, 0, fmt.Errorf("sessao: cancelar sessão: %w", err)
	}
	if linhas[0].Status == statusAgendada { // repetir o cancelamento não audita de novo
		if err := auditoria.Registrar(ctx, tx, auditoria.SessaoCancelada, auditoria.ID(id), s.cfg.Agora()); err != nil {
			return 0, 0, err
		}
	}
	if n, err = s.pedidos.CancelarPedidosDaSessao(ctx, tx, id); err != nil {
		return 0, 0, fmt.Errorf("sessao: estornar pedidos: %w", err)
	}
	return linhas[0].FilmeID, n, nil
}

// erroDaSala traduz o erro da escrita da sala (nome duplicado → 409).
func erroDaSala(err error, op string) error {
	switch {
	case errors.Is(err, ErrSalaNaoEncontrada):
		return err
	case db.EhNomeDuplicado(err):
		return ErrNomeSalaEmUso
	}
	return fmt.Errorf("sessao: %s sala: %w", op, err)
}

// naTx roda fn numa TX do pool com as queries ligadas a ela: toda mutação do
// operador grava a trilha de auditoria na mesma TX (PRD 0037).
func (s *Servico) naTx(ctx context.Context, fn func(q *db.Queries, tx outbox.Tx) error) error {
	if s.cfg.Pool == nil {
		return errors.New("sessao: mutação sem pool")
	}
	return outbox.WithTx(ctx, s.cfg.Pool, func(tx outbox.Tx) error { return fn(s.q.WithTx(tx), tx) })
}

// statusAgendada é o status da sessão em programação (CHECK da migration 008).
const statusAgendada = "agendada"

// SessaoPublica é a sessão vista pelo público (PRD 0014 RF01) — sem nenhum
// campo de filme (o SPA já tem o filme; fronteira ADR 0003).
type SessaoPublica struct {
	ID            int64     `json:"id"`
	SalaID        int64     `json:"sala_id"`
	SalaNome      string    `json:"sala_nome"`
	Inicio        time.Time `json:"inicio"`
	Fim           time.Time `json:"fim"`
	PrecoCentavos int32     `json:"preco_centavos"`
}

// ListarSessoesPublicas devolve as sessões agendadas futuras do filme, com
// cache read-through; o que vem do cache é refiltrado pelo horário atual
// (sessão que começou durante o TTL nunca aparece — RF02).
func (s *Servico) ListarSessoesPublicas(ctx context.Context, filmeID int64) ([]SessaoPublica, error) {
	agora := s.cfg.Agora()
	if lista, ok := s.lerCache(ctx, filmeID); ok {
		return futuras(lista, agora), nil
	}
	linhas, err := s.q.ListarSessoesFuturasDoFilme(ctx, db.ListarSessoesFuturasDoFilmeParams{FilmeID: filmeID, Inicio: agora})
	if err != nil {
		return nil, fmt.Errorf("sessao: listar sessões públicas: %w", err)
	}
	lista := make([]SessaoPublica, len(linhas))
	for i, l := range linhas {
		lista[i] = SessaoPublica{ID: l.ID, SalaID: l.SalaID, SalaNome: l.SalaNome, Inicio: l.Inicio, Fim: l.Fim, PrecoCentavos: l.PrecoCentavos}
	}
	s.gravarCache(ctx, filmeID, lista)
	return lista, nil
}

// MapaSessao é o mapa público de uma sessão (PRD 0014 RF04) — base do E4.
type MapaSessao struct {
	SessaoID int64     `json:"sessao_id"`
	SalaID   int64     `json:"sala_id"`
	SalaNome string    `json:"sala_nome"`
	Fileiras int       `json:"fileiras"`
	Colunas  int       `json:"colunas"`
	Assentos []Assento `json:"assentos"`
	// PrecoCentavos por assento (o total do pedido é sempre calculado no
	// servidor a partir dele — doc.md §14.2).
	PrecoCentavos int64 `json:"preco_centavos"`
}

// Mapa devolve layout e assentos de uma sessão agendada que não começou.
func (s *Servico) Mapa(ctx context.Context, sessaoID int64) (MapaSessao, error) {
	linhas, err := s.q.BuscarMapaDaSessao(ctx, db.BuscarMapaDaSessaoParams{ID: sessaoID, Inicio: s.cfg.Agora()})
	if err != nil {
		return MapaSessao{}, fmt.Errorf("sessao: mapa: %w", err)
	}
	if len(linhas) == 0 {
		return MapaSessao{}, ErrSessaoNaoEncontrada
	}
	var layout Layout
	if err := json.Unmarshal(linhas[0].Layout, &layout); err != nil {
		return MapaSessao{}, fmt.Errorf("sessao: layout ilegível: %w", err)
	}
	return MapaSessao{SessaoID: linhas[0].ID, SalaID: linhas[0].SalaID, SalaNome: linhas[0].SalaNome,
		Fileiras: layout.Fileiras, Colunas: layout.Colunas, Assentos: layout.Assentos(), PrecoCentavos: int64(linhas[0].PrecoCentavos)}, nil
}

// AssentosDaSessaoAberta é a porta do módulo reserva (PRD 0015 RF02):
// códigos dos assentos de uma sessão agendada que ainda não começou.
// ok=false quando a sessão não existe, foi cancelada ou já começou.
func (s *Servico) AssentosDaSessaoAberta(ctx context.Context, sessaoID int64) ([]string, bool, error) {
	m, err := s.Mapa(ctx, sessaoID)
	if errors.Is(err, ErrSessaoNaoEncontrada) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	codigos := make([]string, 0, len(m.Assentos))
	for _, a := range m.Assentos {
		codigos = append(codigos, a.Codigo)
	}
	return codigos, true, nil
}

// PrecoDaSessaoAberta é a porta do módulo pedido (PRD 0023 RF03): preço por
// assento de uma sessão agendada que ainda não começou. ok=false quando a
// sessão não existe, foi cancelada ou já começou.
func (s *Servico) PrecoDaSessaoAberta(ctx context.Context, sessaoID int64) (int64, bool, error) {
	m, err := s.Mapa(ctx, sessaoID)
	if errors.Is(err, ErrSessaoNaoEncontrada) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return m.PrecoCentavos, true, nil
}

// SessaoDoIngresso são os dados de uma sessão que o ingresso mostra.
type SessaoDoIngresso struct {
	FilmeID   int64
	Inicio    time.Time
	Sala      string
	Cancelada bool
}

// DadosParaIngresso é a porta da notificação (PRD 0028): sessão de um
// ingresso já vendido, em qualquer status. ok=false quando não existe.
func (s *Servico) DadosParaIngresso(ctx context.Context, sessaoID int64) (SessaoDoIngresso, bool, error) {
	linhas, err := s.q.BuscarSessaoParaIngresso(ctx, sessaoID)
	if err != nil {
		return SessaoDoIngresso{}, false, fmt.Errorf("sessao: dados do ingresso: %w", err)
	}
	if len(linhas) == 0 {
		return SessaoDoIngresso{}, false, nil
	}
	l := linhas[0]
	return SessaoDoIngresso{FilmeID: l.FilmeID, Inicio: l.Inicio, Sala: l.SalaNome, Cancelada: l.Status != statusAgendada}, true, nil
}

// InicioDaSessao é a porta do pedido (PRD 0036 RF09): início e se a sessão
// foi cancelada, em qualquer status. ok=false quando não existe.
func (s *Servico) InicioDaSessao(ctx context.Context, sessaoID int64) (time.Time, bool, bool, error) {
	d, ok, err := s.DadosParaIngresso(ctx, sessaoID)
	return d.Inicio, d.Cancelada, ok, err
}

func futuras(lista []SessaoPublica, agora time.Time) []SessaoPublica {
	out := make([]SessaoPublica, 0, len(lista))
	for _, l := range lista {
		if l.Inicio.After(agora) {
			out = append(out, l)
		}
	}
	return out
}

func (s *Servico) lerCache(ctx context.Context, filmeID int64) ([]SessaoPublica, bool) {
	if s.cfg.Cache == nil {
		return nil, false
	}
	dados, err := s.cfg.Cache.Get(ctx, chaveCacheFilme(filmeID))
	if err != nil || dados == nil {
		return nil, false
	}
	var lista []SessaoPublica
	if err := json.Unmarshal(dados, &lista); err != nil {
		return nil, false
	}
	return lista, true
}

func (s *Servico) gravarCache(ctx context.Context, filmeID int64, lista []SessaoPublica) {
	if s.cfg.Cache == nil {
		return
	}
	if dados, err := json.Marshal(lista); err == nil {
		_ = s.cfg.Cache.Set(ctx, chaveCacheFilme(filmeID), dados, ttlCachePublico) // best effort
	}
}

// invalidarFilme apaga a lista pública do filme após uma escrita (RF03).
func (s *Servico) invalidarFilme(ctx context.Context, filmeID int64) {
	if s.cfg.Cache == nil {
		return
	}
	if err := s.cfg.Cache.Delete(ctx, chaveCacheFilme(filmeID)); err != nil {
		s.logger.Warn("não foi possível invalidar as sessões públicas em cache (TTL limita)", zap.Error(err))
	}
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

// mesmoLayout compara layouts ignorando a ordem de vaos/pcd (auditoria
// 0013): reenviar o mesmo layout reordenado não conta como mudança.
func mesmoLayout(a, b Layout) bool {
	ja, _ := json.Marshal(normalizado(a))
	jb, _ := json.Marshal(normalizado(b))
	return string(ja) == string(jb)
}

func normalizado(l Layout) Layout {
	ordenar := func(ps []Posicao) []Posicao {
		out := append([]Posicao(nil), ps...)
		sort.Slice(out, func(i, j int) bool {
			if out[i].Fileira != out[j].Fileira {
				return out[i].Fileira < out[j].Fileira
			}
			return out[i].Coluna < out[j].Coluna
		})
		return out
	}
	return Layout{Fileiras: l.Fileiras, Colunas: l.Colunas, Vaos: ordenar(l.Vaos), PCD: ordenar(l.PCD)}
}

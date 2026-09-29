// Package identidade é o módulo dono dos usuários (PRD 0009, refinamento E1):
// registro de cliente, login com Argon2id e anti-enumeração, dados do próprio
// usuário e criação do operador por CLI. Transaction script (ADR 0005). A
// emissão/validação de token e a autorização por papel são da plataforma
// internal/autenticacao — outros módulos nunca importam este pacote.
package identidade

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/autenticacao"
	"github.com/mclovin137/morfeu/internal/identidade/db"
)

// Limites de entrada (RF02).
const (
	nomeMax      = 120
	emailMax     = 254
	senhaMin     = 8
	senhaMax     = 128
	prefixoConta = "conta:"
	prefixoIP    = "ip:"
	prefixoReg   = "registro:ip:"
)

// Limitador é o que o serviço precisa do limitador da plataforma
// (autenticacao.Limitador) — interface no consumidor (ADR 0003).
type Limitador interface {
	Bloqueado(ctx context.Context, chave string) bool
	RegistrarFalha(ctx context.Context, chave string)
	Limpar(ctx context.Context, chave string)
}

// Config agrupa as dependências ajustáveis do serviço.
type Config struct {
	Argon2           ParametrosArgon2
	HashConcorrencia int
	LimiteConta      Limitador // falhas de login por conta (5/5 min)
	LimiteIP         Limitador // falhas de login por IP (20/5 min)
	LimiteRegistro   Limitador // cadastros por IP (10/1 h)
}

// Usuario é a visão pública do usuário (sem hash).
type Usuario struct {
	ID    uuid.UUID          `json:"id"`
	Nome  string             `json:"nome"`
	Email string             `json:"email"`
	Papel autenticacao.Papel `json:"papel"`
}

// Sessao é o resultado do login.
type Sessao struct {
	AccessToken string
	ExpiraEm    time.Time
}

// Servico implementa os casos de uso de identidade.
type Servico struct {
	q         *db.Queries
	emissor   *autenticacao.Emissor
	metricas  *autenticacao.Metricas
	logger    *zap.Logger
	cfg       Config
	sem       chan struct{}
	hashDummy string
	// verificar é trocável só nos testes do pacote (CA03: prova de que o
	// hash roda nos dois ramos do login).
	verificar func(senha, hash string) (bool, error)
	agora     func() time.Time
}

// NovoServico prepara o serviço e o hash dummy usado quando o e-mail não
// existe (mesmos parâmetros → mesmo custo — anti-enumeração, RF03).
func NovoServico(q *db.Queries, emissor *autenticacao.Emissor, cfg Config, metricas *autenticacao.Metricas, logger *zap.Logger) (*Servico, error) {
	if cfg.HashConcorrencia <= 0 || cfg.LimiteConta == nil || cfg.LimiteIP == nil || cfg.LimiteRegistro == nil {
		return nil, fmt.Errorf("identidade: configuração incompleta")
	}
	aleatorio := make([]byte, 32)
	if _, err := rand.Read(aleatorio); err != nil {
		return nil, fmt.Errorf("identidade: gerar hash dummy: %w", err)
	}
	dummy, err := gerarHash(hex.EncodeToString(aleatorio), cfg.Argon2)
	if err != nil {
		return nil, err
	}
	return &Servico{
		q: q, emissor: emissor, metricas: metricas, logger: logger, cfg: cfg,
		sem: make(chan struct{}, cfg.HashConcorrencia), hashDummy: dummy,
		verificar: verificarHash, agora: time.Now,
	}, nil
}

// EntradaRegistro são os dados do cadastro. Não há campo de papel: cliente
// sempre (RN01).
type EntradaRegistro struct {
	Nome  string
	Email string
	Senha string
}

// Registrar cria um cliente (RF02). Limitado por IP e pelo semáforo.
func (s *Servico) Registrar(ctx context.Context, in EntradaRegistro, ip string) (Usuario, error) {
	nome, email, err := validarRegistro(in)
	if err != nil {
		return Usuario{}, err
	}
	chaveIP := prefixoReg + ip
	if s.cfg.LimiteRegistro.Bloqueado(ctx, chaveIP) {
		return Usuario{}, ErrMuitasTentativas
	}
	s.cfg.LimiteRegistro.RegistrarFalha(ctx, chaveIP) // conta a tentativa de cadastro

	liberar, ok := s.ocuparSemaforo()
	if !ok {
		return Usuario{}, ErrSaturado
	}
	hash, err := gerarHash(in.Senha, s.cfg.Argon2)
	liberar()
	if err != nil {
		return Usuario{}, err
	}
	return inserirUsuario(ctx, s.q, s.logger, nome, email, hash, autenticacao.PapelCliente)
}

// Login autentica e emite o access token (RF03). Toda falha de credencial
// devolve ErrCredenciaisInvalidas, com o mesmo custo de hash (dummy quando o
// e-mail não existe).
func (s *Servico) Login(ctx context.Context, email, senha, ip string) (Sessao, error) {
	inicio := s.agora()
	email = normalizarEmail(email)
	chaveConta, chaveIP := prefixoConta+email, prefixoIP+ip

	if s.cfg.LimiteConta.Bloqueado(ctx, chaveConta) {
		s.registrarBloqueio(ctx, autenticacao.EscopoConta, inicio)
		return Sessao{}, ErrMuitasTentativas
	}
	if s.cfg.LimiteIP.Bloqueado(ctx, chaveIP) {
		s.registrarBloqueio(ctx, autenticacao.EscopoIP, inicio)
		return Sessao{}, ErrMuitasTentativas
	}

	liberar, ok := s.ocuparSemaforo()
	if !ok {
		s.metricas.Login(ctx, autenticacao.ResultadoSaturado, s.agora().Sub(inicio))
		return Sessao{}, ErrSaturado
	}
	usuario, valido := s.conferirCredenciais(ctx, email, senha)
	liberar()

	if !valido {
		s.cfg.LimiteConta.RegistrarFalha(ctx, chaveConta)
		s.cfg.LimiteIP.RegistrarFalha(ctx, chaveIP)
		s.metricas.Login(ctx, autenticacao.ResultadoCredenciaisInvalidas, s.agora().Sub(inicio))
		return Sessao{}, ErrCredenciaisInvalidas
	}

	s.cfg.LimiteConta.Limpar(ctx, chaveConta)
	token, expira, err := s.emissor.Emitir(usuario.ID, autenticacao.Papel(usuario.Papel))
	if err != nil {
		return Sessao{}, err
	}
	s.metricas.Login(ctx, autenticacao.ResultadoSucesso, s.agora().Sub(inicio))
	s.logger.Info("login", zap.String("usuario_id", usuario.ID.String()))
	return Sessao{AccessToken: token, ExpiraEm: expira}, nil
}

// conferirCredenciais roda o Argon2id SEMPRE — contra o hash do usuário ou
// contra o dummy — para o tempo não revelar se o e-mail existe.
func (s *Servico) conferirCredenciais(ctx context.Context, email, senha string) (db.BuscarUsuarioPorEmailRow, bool) {
	if utf8.RuneCountInString(senha) > senhaMax {
		// Recusa antes do hash (DoS). O tempo diferente depende só do tamanho
		// do input do atacante, não da existência da conta.
		return db.BuscarUsuarioPorEmailRow{}, false
	}
	linhas, err := s.q.BuscarUsuarioPorEmail(ctx, email)
	if err != nil || len(linhas) == 0 {
		if err != nil {
			s.logger.Error("login: falha ao buscar usuário", zap.Error(err))
		}
		_, _ = s.verificar(senha, s.hashDummy)
		return db.BuscarUsuarioPorEmailRow{}, false
	}
	ok, err := s.verificar(senha, linhas[0].SenhaHash)
	if err != nil {
		s.logger.Error("login: hash armazenado inválido", zap.String("usuario_id", linhas[0].ID.String()))
		return db.BuscarUsuarioPorEmailRow{}, false
	}
	return linhas[0], ok
}

// Eu devolve os dados do usuário autenticado (RF04).
func (s *Servico) Eu(ctx context.Context, id uuid.UUID) (Usuario, error) {
	linhas, err := s.q.BuscarUsuarioPorID(ctx, id)
	if err != nil {
		return Usuario{}, fmt.Errorf("identidade: buscar usuário: %w", err)
	}
	if len(linhas) == 0 {
		return Usuario{}, ErrUsuarioNaoEncontrado
	}
	u := linhas[0]
	return Usuario{ID: u.ID, Nome: u.Nome, Email: u.Email, Papel: autenticacao.Papel(u.Papel)}, nil
}

// SeedOperador cria o operador com senha aleatória (RF05) — usado só pelo
// subcomando CLI, fora do serviço HTTP. Idempotente: se o e-mail já existe,
// nada muda e criado=false. A senha só é devolvida na criação; o chamador a
// exibe uma única vez e nunca a loga.
func SeedOperador(ctx context.Context, q *db.Queries, argon ParametrosArgon2, logger *zap.Logger, nome, email string) (senha string, criado bool, err error) {
	senha, err = gerarSenhaAleatoria()
	if err != nil {
		return "", false, err
	}
	nomeN, emailN, err := validarRegistro(EntradaRegistro{Nome: nome, Email: email, Senha: senha})
	if err != nil {
		return "", false, err
	}
	hash, err := gerarHash(senha, argon)
	if err != nil {
		return "", false, err
	}
	if _, err := inserirUsuario(ctx, q, logger, nomeN, emailN, hash, autenticacao.PapelOperador); err != nil {
		if errors.Is(err, ErrEmailEmUso) {
			return "", false, nil
		}
		return "", false, err
	}
	return senha, true, nil
}

func inserirUsuario(ctx context.Context, q *db.Queries, logger *zap.Logger, nome, email, hash string, papel autenticacao.Papel) (Usuario, error) {
	id := uuid.New()
	n, err := q.InserirUsuario(ctx, db.InserirUsuarioParams{
		ID: id, Nome: nome, Email: email, SenhaHash: hash, Papel: string(papel),
	})
	if err != nil {
		return Usuario{}, fmt.Errorf("identidade: inserir usuário: %w", err)
	}
	if n == 0 {
		return Usuario{}, ErrEmailEmUso
	}
	logger.Info("usuário criado", zap.String("usuario_id", id.String()), zap.String("papel", string(papel)))
	return Usuario{ID: id, Nome: nome, Email: email, Papel: papel}, nil
}

// ocuparSemaforo tenta uma vaga de hashing sem esperar (RF07).
func (s *Servico) ocuparSemaforo() (liberar func(), ok bool) {
	select {
	case s.sem <- struct{}{}:
		return func() { <-s.sem }, true
	default:
		return nil, false
	}
}

func (s *Servico) registrarBloqueio(ctx context.Context, e autenticacao.Escopo, inicio time.Time) {
	s.metricas.Bloqueio(ctx, e)
	s.metricas.Login(ctx, autenticacao.ResultadoBloqueado, s.agora().Sub(inicio))
}

func normalizarEmail(e string) string {
	return strings.ToLower(strings.TrimSpace(e))
}

// validarRegistro normaliza e valida (RF02); devolve os campos inválidos
// sem ecoar valores.
func validarRegistro(in EntradaRegistro) (nome, email string, err error) {
	nome = strings.TrimSpace(in.Nome)
	email = normalizarEmail(in.Email)
	var campos []string
	if n := utf8.RuneCountInString(nome); n == 0 || n > nomeMax {
		campos = append(campos, "nome")
	}
	if !emailValido(email) {
		campos = append(campos, "email")
	}
	if n := utf8.RuneCountInString(in.Senha); n < senhaMin || n > senhaMax {
		campos = append(campos, "senha")
	}
	if len(campos) > 0 {
		return "", "", &ErroValidacao{Campos: campos}
	}
	return nome, email, nil
}

func emailValido(email string) bool {
	if email == "" || len(email) > emailMax {
		return false
	}
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Name == "" && addr.Address == email
}

package autenticacao

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// ConfigLimitador parametriza o Limitador (RF06). Agora é injetável para
// testes sem sleep; a janela no Redis usa o TTL real do servidor.
type ConfigLimitador struct {
	Redis   redis.Cmdable
	Prefixo string
	Max     int
	Janela  time.Duration
	Agora   func() time.Time
}

// Limitador conta falhas por chave numa janela fixa. Fonte primária: Redis
// (INCR+EXPIRE). Se o Redis falhar, usa um contador em memória do processo —
// decisão do usuário no refinamento E1: nunca fica sem limite e nunca derruba
// o login. As chaves são gravadas como SHA-256 (e-mail/IP nunca em claro).
type Limitador struct {
	cfg    ConfigLimitador
	logger *zap.Logger

	mu      sync.Mutex
	memoria map[string]entradaMemoria
}

// maxEntradasMemoria limita o fallback: sob ataque com o Redis fora, cada
// IP/conta vira uma entrada. Acima do teto, as expiradas são varridas; se
// ainda assim estiver cheio, a chave nova é tratada como bloqueada (nunca
// fica sem limite — decisão do refinamento E1).
const maxEntradasMemoria = 10_000

type entradaMemoria struct {
	falhas int
	expira time.Time
}

// NovoLimitador cria o limitador; Max ≤ 0 ou Janela ≤ 0 são erro de
// programação e caem em ErrConfigInvalida.
func NovoLimitador(cfg ConfigLimitador, logger *zap.Logger) (*Limitador, error) {
	if cfg.Max <= 0 || cfg.Janela <= 0 || cfg.Redis == nil {
		return nil, ErrConfigInvalida
	}
	if cfg.Agora == nil {
		cfg.Agora = time.Now
	}
	return &Limitador{cfg: cfg, logger: logger, memoria: map[string]entradaMemoria{}}, nil
}

// Bloqueado informa se a chave atingiu Max falhas dentro da janela.
func (l *Limitador) Bloqueado(ctx context.Context, chave string) bool {
	k := l.chaveRedis(chave)
	n, err := l.cfg.Redis.Get(ctx, k).Int()
	switch {
	case err == nil:
		return n >= l.cfg.Max
	case errors.Is(err, redis.Nil):
		// Redis saudável sem a chave: vale só o que sobrou de uma queda anterior.
		return l.falhasEmMemoria(k, false) >= l.cfg.Max
	default:
		l.avisarFallback(err)
		return l.falhasEmMemoria(k, true) >= l.cfg.Max
	}
}

// RegistrarFalha incrementa o contador da chave; a 1ª falha abre a janela.
func (l *Limitador) RegistrarFalha(ctx context.Context, chave string) {
	k := l.chaveRedis(chave)
	var incr *redis.IntCmd
	_, err := l.cfg.Redis.TxPipelined(ctx, func(p redis.Pipeliner) error {
		incr = p.Incr(ctx, k)
		p.ExpireNX(ctx, k, l.cfg.Janela)
		return nil
	})
	if err == nil && incr.Err() == nil {
		return
	}
	l.avisarFallback(err)
	l.incrementarMemoria(k)
}

// Limpar zera a chave (login bem-sucedido).
func (l *Limitador) Limpar(ctx context.Context, chave string) {
	k := l.chaveRedis(chave)
	if err := l.cfg.Redis.Del(ctx, k).Err(); err != nil {
		l.avisarFallback(err)
	}
	l.mu.Lock()
	delete(l.memoria, k)
	l.mu.Unlock()
}

func (l *Limitador) chaveRedis(chave string) string {
	soma := sha256.Sum256([]byte(chave))
	return l.cfg.Prefixo + hex.EncodeToString(soma[:])
}

// falhasEmMemoria lê o contador local. emFallback=true (Redis fora): com o
// mapa cheio, chave desconhecida conta como bloqueada — nunca sem limite.
func (l *Limitador) falhasEmMemoria(k string, emFallback bool) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.memoria[k]
	if !ok {
		if emFallback && len(l.memoria) >= maxEntradasMemoria {
			return l.cfg.Max
		}
		return 0
	}
	if !l.cfg.Agora().Before(e.expira) {
		delete(l.memoria, k)
		return 0
	}
	return e.falhas
}

func (l *Limitador) incrementarMemoria(k string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	agora := l.cfg.Agora()
	e, ok := l.memoria[k]
	if !ok && len(l.memoria) >= maxEntradasMemoria {
		l.varrerExpiradas(agora)
		if len(l.memoria) >= maxEntradasMemoria {
			return
		}
	}
	if !ok || !agora.Before(e.expira) {
		e = entradaMemoria{expira: agora.Add(l.cfg.Janela)}
	}
	e.falhas++
	l.memoria[k] = e
}

func (l *Limitador) varrerExpiradas(agora time.Time) {
	for k, e := range l.memoria {
		if !agora.Before(e.expira) {
			delete(l.memoria, k)
		}
	}
}

// avisarFallback registra a degradação sem a chave (pode derivar de e-mail).
func (l *Limitador) avisarFallback(err error) {
	l.logger.Warn("limitador de tentativas usando memória (Redis indisponível)", zap.Error(err))
}

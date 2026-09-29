package reserva

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/reserva/db"
)

// ttlOcupacao: defasagem máxima da ocupação pública (refinamento E4: 3 s sem
// invalidação — menor que o polling de 3–5 s do E5; o 409 da trava é a
// verdade final). Invalidar a cada hold zeraria o cache justo sob disputa.
const ttlOcupacao = 3 * time.Second

func chaveOcupacao(sessaoID int64) string {
	return fmt.Sprintf("reserva:ocupacao:%d", sessaoID)
}

// Ocupacao é a visão pública: só os códigos ocupados (nada sobre donos).
type Ocupacao struct {
	SessaoID int64    `json:"sessao_id"`
	Ocupados []string `json:"ocupados"`
}

// Ocupacao devolve os assentos com hold vivo (PRD 0016 RF01–RF03). No hit do
// cache nem a porta de sessões é consultada; no miss, sessão indisponível →
// ErrSessaoIndisponivel e nada é cacheado.
func (s *Servico) Ocupacao(ctx context.Context, sessaoID int64) (Ocupacao, error) {
	if ocupados, ok := s.lerOcupacao(ctx, sessaoID); ok {
		return Ocupacao{SessaoID: sessaoID, Ocupados: ocupados}, nil
	}
	_, ok, err := s.cfg.Sessoes.AssentosDaSessaoAberta(ctx, sessaoID)
	if err != nil {
		return Ocupacao{}, fmt.Errorf("reserva: consultar sessão: %w", err)
	}
	if !ok {
		return Ocupacao{}, ErrSessaoIndisponivel
	}
	ocupados, err := db.New(s.pool).OcupadosDaSessao(ctx, db.OcupadosDaSessaoParams{SessaoID: sessaoID, Agora: s.cfg.Agora()})
	if err != nil {
		return Ocupacao{}, fmt.Errorf("reserva: ocupação: %w", err)
	}
	if ocupados == nil {
		ocupados = []string{}
	}
	s.gravarOcupacao(ctx, sessaoID, ocupados)
	return Ocupacao{SessaoID: sessaoID, Ocupados: ocupados}, nil
}

func (s *Servico) lerOcupacao(ctx context.Context, sessaoID int64) ([]string, bool) {
	if s.cfg.Cache == nil {
		return nil, false
	}
	dados, err := s.cfg.Cache.Get(ctx, chaveOcupacao(sessaoID))
	if err != nil || dados == nil {
		return nil, false
	}
	var ocupados []string
	if err := json.Unmarshal(dados, &ocupados); err != nil || ocupados == nil {
		return nil, false
	}
	return ocupados, true
}

// gravarOcupacao: falha do cache só custa o próximo miss (RF03).
func (s *Servico) gravarOcupacao(ctx context.Context, sessaoID int64, ocupados []string) {
	if s.cfg.Cache == nil {
		return
	}
	dados, err := json.Marshal(ocupados)
	if err != nil {
		return
	}
	if err := s.cfg.Cache.Set(ctx, chaveOcupacao(sessaoID), dados, ttlOcupacao); err != nil {
		s.logger.Warn("reserva: gravar cache de ocupação", zap.Error(err))
	}
}

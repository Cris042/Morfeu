package autenticacao

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Resultado do login (valores fechados — label de baixa cardinalidade).
type Resultado string

// Resultados possíveis de uma tentativa de login (RF07).
const (
	ResultadoSucesso              Resultado = "sucesso"
	ResultadoCredenciaisInvalidas Resultado = "credenciais_invalidas" //nolint:gosec // G101: rótulo de métrica, não credencial
	ResultadoBloqueado            Resultado = "bloqueado"
	ResultadoSaturado             Resultado = "saturado"
)

// Escopo do bloqueio do limitador.
type Escopo string

// Escopos de bloqueio.
const (
	EscopoConta Escopo = "conta"
	EscopoIP    Escopo = "ip"
)

// Metricas agrupa os instrumentos de auth (RF07). Sem label de usuário, e-mail
// ou IP — só resultado/escopo, ambos na allowlist da telemetria (0006).
type Metricas struct {
	login        metric.Int64Counter
	loginDuracao metric.Float64Histogram
	bloqueios    metric.Int64Counter
	reuso        metric.Int64Counter
}

// NovasMetricas registra os instrumentos no MeterProvider global.
func NovasMetricas() (*Metricas, error) {
	m := otel.Meter("morfeu/autenticacao")
	var (
		met Metricas
		err error
	)
	if met.login, err = m.Int64Counter("auth_login_total",
		metric.WithDescription("Tentativas de login por resultado.")); err != nil {
		return nil, fmt.Errorf("autenticacao: métrica login: %w", err)
	}
	if met.loginDuracao, err = m.Float64Histogram("auth_login_duracao_segundos",
		// Sem WithUnit: o exporter Prometheus anexaria "_seconds" ao nome já em PT.
		metric.WithDescription("Duração do login em segundos (inclui Argon2id).")); err != nil {
		return nil, fmt.Errorf("autenticacao: métrica duração: %w", err)
	}
	if met.bloqueios, err = m.Int64Counter("auth_ratelimit_bloqueios_total",
		metric.WithDescription("Tentativas barradas pelo limitador, por escopo.")); err != nil {
		return nil, fmt.Errorf("autenticacao: métrica bloqueios: %w", err)
	}
	if met.reuso, err = m.Int64Counter("auth_refresh_reuso_total",
		metric.WithDescription("Reuso de refresh token detectado (família revogada).")); err != nil {
		return nil, fmt.Errorf("autenticacao: métrica reuso: %w", err)
	}
	return &met, nil
}

// Login registra o resultado e a duração de uma tentativa.
func (m *Metricas) Login(ctx context.Context, r Resultado, d time.Duration) {
	attrs := metric.WithAttributes(attribute.String("resultado", string(r)))
	m.login.Add(ctx, 1, attrs)
	m.loginDuracao.Record(ctx, d.Seconds(), attrs)
}

// Bloqueio registra uma tentativa barrada pelo limitador.
func (m *Metricas) Bloqueio(ctx context.Context, e Escopo) {
	m.bloqueios.Add(ctx, 1, metric.WithAttributes(attribute.String("escopo", string(e))))
}

// ReusoRefresh registra reuso de refresh token (task 0010).
func (m *Metricas) ReusoRefresh(ctx context.Context) {
	m.reuso.Add(ctx, 1)
}

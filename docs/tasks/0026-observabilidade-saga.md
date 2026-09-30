# Task 0026 — Observabilidade da saga + consumidor de notificação + integração ponta a ponta (E6, T5)

- **Data:** 2026-09-30
- **Status:** concluída (auditoria APROVADA na revalidação, 2026-09-30)
- **Branch:** `feature/0026-observabilidade-saga` (da main `b3a496f`)
- **PRD:** docs/prd/0026-observabilidade-saga.md
- **Item do roadmap:** E6 — Saga do checkout (5ª de 6). Refinamento: `docs/refinamentos/E6-saga-checkout.md` §T5; ADR 0010.

## Objetivo

Tornar a saga observável e provar o caminho inteiro: métricas de compensação, presos e latência fim a fim, alertas versionados, a fila e o consumidor (stub) de `pedido.confirmado`, e um teste de integração que vai da trava à notificação processada — incluindo a falha da notificação que nunca desfaz a venda.

## Escopo

`saga_compensacoes_total{passo}`, `pedidos_presos{estado}`, `checkout_confirmado_ate_notificado_segundos`, funil `expirado`; 5 regras de alerta; fila `notificacao.pedido_confirmado` com DLX/DLQ próprias; módulo `notificacao` (stub); teste ponta a ponta com PG/Redis/RabbitMQ reais; Stripe "já estornado" como sucesso; correção do nome do histograma do gateway.

## Fora de escopo

E-mail/QR (E7); replay da DLQ, gauge de DLQ das filas novas, limpeza da outbox e hardening do worker (0027); Tempo e dashboards (E10); limpeza de pedidos abandonados (E11, junto com a de holds — retenção de 12 meses da trilha).

## Arquivos esperados

25 (lista no PRD).

## Dependências esperadas

Nenhuma nova.

## Critérios de aceite

- [x] Trava → pedido → webhook → pivô → outbox → relay → RabbitMQ → consumidor, com dedup e latência medida.
- [x] Notificação falhando → DLQ; pedido segue pago com ingressos.
- [x] Métricas de compensação e presos corretas; 14 regras de alerta provisionadas.
- [x] "Já estornado" do Stripe tratado como sucesso.

## Riscos

- Teste com broker real é mais lento → prazo explícito (`eventualmente`), sem sleep fixo.

## Estimativa de impacto

Médio: nova fila no broker (aditiva) e módulo novo mínimo.

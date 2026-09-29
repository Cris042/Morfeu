# Task 0015 — Reserva: trava de assento (holds) + sweeper (E4, T1)

- **Data:** 2026-09-29
- **Status:** em andamento
- **Branch:** `feature/0015-reserva-trava` (da main `1f7c594`)
- **PRD:** docs/prd/0015-reserva-trava.md
- **Item do roadmap:** E4 — Reserva (task 1/2). Refinamento: `docs/refinamentos/E4-reserva.md` §T1; ADR 0008.

## Objetivo

Criar o módulo `reserva` com a trava de assentos no PostgreSQL (fluxo crítico 2 do `doc.md`). Garantir que, numa disputa pelo mesmo assento, exatamente um dono vença, com expiração lazy, extensão única, teto por dono, rate limit e sweeper de higiene.

## Escopo

Migration `holds`; módulo `internal/reserva` (aggregate/VOs, repository, serviço, handler, sweeper, token de carrinho em cookie); porta `reserva → sessao`; wiring (rotas em api|all, sweeper no worker, métricas, limitadores); depguard; testes de corrida.

## Fora de escopo

Ocupação pública + cache + alertas (0016); conversão em ingresso/pedido (E6); UI (E5); purge de holds terminais (hardening).

## Arquivos esperados

~25 (lista fechada no PRD).

## Dependências esperadas

Nenhuma nova (pgx, sqlc, echo, uuid, zap, OTel e o limitador do E1 já existem).

## Critérios de aceite

- [ ] N=20 donos disputando o mesmo assento → exatamente 1 × 201, 19 × 409, em 10 rodadas; invariante verificada por query.
- [ ] Hold vencido é roubado (relógio adiantado), inclusive sob corrida (exatamente 1 vencedor).
- [ ] Lote tudo ou nada; lotes cruzados concorrentes sem 500.
- [ ] Teto de 6 por dono respeitado sob concorrência; extensão única; posse → 404; CSRF 403; 429.
- [ ] Sweeper marca vencidos como `expirado`; corrida sweeper × roubo sem erro.
- [ ] CI verde.

## Riscos

- Deadlock entre lotes → ordem global por código + retry em 40P01 + teste de lotes cruzados.

## Estimativa de impacto

Alto em código (módulo novo), médio em banco (tabela nova, sem alterar as existentes), nenhum em infra.

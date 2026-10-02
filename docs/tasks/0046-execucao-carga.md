# Task 0046 — Execução local da carga e relatório do M5 indicativo (E12, T2)

- **Data:** 2026-10-01
- **Status:** implementada (auditoria pendente)
- **Branch:** `feature/0046-execucao-carga`
- **PRD:** docs/prd/0046-execucao-carga.md
- **Item do roadmap:** E12 — Teste de carga k6 (2ª e última; última do roadmap). Refinamento: `docs/refinamentos/E12-teste-de-carga.md` §T2.

## Objetivo

Rodar os cenários de carga nesta máquina e registrar o M5 indicativo, que fecha o roadmap (decisão do usuário).

## Escopo

Baseline, ramp, SLO 3×, misto, disputa, checkout e soak, com invariante; relatório; ajustes pequenos com teste; M5 oficial no checklist da E0c-CD.

## Fora de escopo

M5 oficial, Argon2, soak longo (VM — E0c-CD); mudanças estruturais (ADR/task nova).

## Arquivos esperados

~10 (+ ajustes).

## Dependências esperadas

Nenhuma (salvo `singleflight` com evidência).

## Critérios de aceite

Ver PRD (CA01–CA06).

## Riscos

- Números locais lidos como oficiais → selo "indicativo (local)".

## Estimativa de impacto

Baixo no código; ~3 h de execução.

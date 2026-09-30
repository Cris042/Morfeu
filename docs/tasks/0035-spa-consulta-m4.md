# Task 0035 — SPA: consulta e ingresso + E2E do M4 (E8, T5)

- **Data:** 2026-09-30
- **Status:** em andamento
- **Branch:** `feature/0035-spa-consulta-m4` (da main `2a76e67`)
- **PRD:** docs/prd/0035-spa-consulta-m4.md
- **Item do roadmap:** E8 — SPA checkout + convidado/conta (5ª de 5, fecha o E8). **Fecha o M4.** Refinamento: `docs/refinamentos/E8-spa-checkout.md` §T5.

## Objetivo

Telas de consulta de convidado e do ingresso (link do e-mail) e a prova do M4: jornada completa no navegador, com e-mail verificável, sob a CSP de produção e com varredura de acessibilidade.

## Escopo

`/consulta`, `/i/:ref`, links para a consulta, `referrerPolicy: no-referrer` no cliente, rota de teste `GET /__teste/emails` (só com fakes), E2E `m4.spec.ts` com axe e vigia de CSP, CSP no `vite preview` + gate de igualdade com o Caddy, snippet `(ingresso)` do Caddy.

## Fora de escopo

Deploy/Caddy real (E0c-CD); Stripe real no E2E (sem rede externa no CI).

## Arquivos esperados

26 (lista no PRD).

## Dependências esperadas

`@axe-core/playwright` 4.13.0 (dev) — decisão do usuário no refinamento E8.

## Critérios de aceite

- [ ] Consulta com mensagem única de erro; ingresso com estados válido/usado/404/410 e QR sem referrer.
- [ ] E2E M4 (convidado e conta) verde no CI, sem violação de CSP nem do axe; M3 intacto.

## Riscos

- Falsos positivos do axe → a varredura aponta seletor e motivo; o 1º achado foi real (contraste do rodapé, corrigido).

## Estimativa de impacto

Médio: telas + E2E; backend só a rota de teste.

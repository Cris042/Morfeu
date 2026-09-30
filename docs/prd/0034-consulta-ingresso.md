# PRD 0034 — Backend: consulta de convidado e página do ingresso (E8, T4)

- **Task:** docs/tasks/0034-consulta-ingresso.md
- **Branch:** feature/0034-consulta-ingresso
- **Data:** 2026-09-30
- **Status:** em andamento

## Objetivo

O convidado não tem conta: recupera o pedido por e-mail + código (80 bits) e abre o ingresso pelo link do e-mail. Fontes: `docs/refinamentos/E8-spa-checkout.md` §T4, exigências do E6/E7 (token `HMAC(seg_v, "ingresso:v1:"+id)`, respostas indistinguíveis, 410 só após HMAC), ADR 0003 (portas compostas no main), ADR 0010.

## Escopo

Rotas, serviço, portas, rate limits, headers, redação e testes.

## Fora de escopo

- Telas da consulta e do ingresso, fallback `/i/*` no Caddy (0035 / E0c-CD).
- Marcar ingresso como usado (check-in no E9).

## Requisitos funcionais

- RF01 — `POST /pedidos/consulta {email, codigo}` (anti-CSRF, corpo ≤ 16 KB, campos desconhecidos recusados, `Cache-Control: no-store` em toda resposta):
  - código normalizado (maiúsculas, sem espaços/hífens) e validado `^[A-Z2-7]{16}$`; e-mail normalizado (trim + minúsculas) dos dois lados;
  - comparação `subtle.ConstantTimeCompare` de SHA-256; código inexistente/malformado compara contra um e-mail fantasma — **mesmo caminho**;
  - todo "não encontrado" → `404 {"erro":"nao_encontrado"}`, idêntico em status, corpo e headers;
  - acerto → `200 {pedido, ingressos:[{assento, ref}]}`, links só de ingressos **ativos** de pedido **pago**; nunca `client_secret`/cobrança.
- RF02 — Rate limit da consulta: 10/min por IP e 5/min por e-mail normalizado (chave = SHA-256 do e-mail; nunca em claro no Redis) → 429.
- RF03 — `GET /i/{ref}` com `ref = {uuid canônico}.{token base64url de 43}` (parse estrito: minúsculas, sem padding, sem outro formato de UUID):
  - HMAC com o segredo da `versao_token` do ingresso (versão 1 quando o id não existe — **calculado sempre**) + `hmac.Equal`;
  - malformado, inexistente ou token errado → mesmo 404;
  - **só depois do token válido**: ingresso `cancelado`, pedido não `pago` ou agora > início da sessão + 24 h → `410 {"erro":"ingresso_indisponivel"}`;
  - válido → `200 {assento, status, filme, sala, inicio}`.
- RF04 — `GET /i/{ref}/qr.png`: mesma validação; PNG do QR com o **mesmo link do e-mail** (`BASE_URL_PUBLICA + /i/ + ref`).
- RF05 — Headers em toda resposta de `/i/*` (inclusive 404/410/429): `Referrer-Policy: no-referrer`, `Cache-Control: no-store`, `X-Content-Type-Options: nosniff`. GETs idempotentes (scanners de e-mail).
- RF06 — Rate limit por IP de `/i/*`: 30/min.
- RF07 — Redação: o access log grava o template da rota (`/i/:ref`, `/i/:ref/qr.png`; sem rota → `/i/[redigido]`) e o `url.path` do span do otelecho é sobrescrito com o mesmo valor. Métricas já usam o template da rota.
- RF08 — Portas (ADR 0003): `ConfigConsulta.Sessao` (sessao + catalogo, composta no main) e `ConfigConsulta.QR` (`notificacao.QR`, o mesmo gerador do e-mail). `Config.Consulta == nil` → rotas desligadas; configurada → exige limitadores, portas, URL base e o segredo da versão 1.

## Requisitos não funcionais

- RNF01 — Nenhum link, token, e-mail ou código em log.
- RNF02 — Depguard do `pedido` inalterado (nada de otel, QR ou outros módulos).

## Regras de negócio

- RN01 — Validade do link: início da sessão + 24 h.
- RN02 — Revogação por status (ingresso cancelado ou pedido não pago) — sem lista de revogação.

## Critérios de aceite

- [ ] CA01 — Consulta com e-mail em outra caixa e código digitado com hífen → pedido + 2 refs válidas; sem dados da cobrança; `no-store`.
- [ ] CA02 — E-mail errado, código inexistente, malformado e vazio → respostas idênticas; pedido pendente → sem links; sem anti-CSRF → 403.
- [ ] CA03 — Teto da consulta por e-mail normalizado.
- [ ] CA04 — Link válido → dados + QR com o link do e-mail; 10 variantes inválidas (token trocado, de outro id, segredo errado, sem ponto, UUID maiúsculo, curto, com padding, lixo, QR errado/inexistente) idênticas ao 404 de id inexistente.
- [ ] CA05 — Expirado (início + 24 h + 1 s) → 410 com token certo e 404 com token errado; cancelado → 410 (página e QR) e some da consulta.
- [ ] CA06 — Token da versão 2 valida com o segredo da v2; token da v1 num ingresso v2 → 404.
- [ ] CA07 — Teto por IP em `/i/*` com os headers.
- [ ] CA08 — Log e span sem o token (teste de unidade com otelecho real + mutação conferida).
- [ ] CA09 — Lint, suíte `-race` e CI verdes.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Consulta, ingresso, QR, versões, limites (PG + Redis reais, rotas reais) | integração (`pedido`) | CA01–CA07 |
| Redação no log e no span | unit (`cmd/morfeu`) | CA08 |

## Plano de implementação

1. Queries (`PedidoPorCodigo`, `IngressoParaPagina`) + sqlc.
2. `consulta.go` (serviço), handler, `Config.Consulta`.
3. Main: porta da sessão, `notificacao.QR`, limitadores, redação.
4. Testes, docs.

**Skills de apoio (§4.4):** `security-review`, `golang-testing`.

## Arquivos que serão criados

- `internal/pedido/consulta.go`, `internal/pedido/consulta_test.go`, `cmd/morfeu/redacao_test.go`
- `docs/tasks/0034-consulta-ingresso.md`, `docs/prd/0034-consulta-ingresso.md`

## Arquivos que serão modificados

- `internal/pedido/{queries.sql, service.go, handler.go, handler_test.go}`, `internal/pedido/db/queries.sql.go` (gerado)
- `internal/notificacao/email.go` (`QR` exportado), `cmd/morfeu/main.go`
- `docs/ambiente-dev.md`, `docs/tasks/README.md`, `plan.md`, `state.md`

Total: 16.

## Dependências utilizadas

Nenhuma nova.

## Impactos técnicos

- 3 limitadores novos no Redis (prefixos `consulta-ip`, `consulta-email`, `ingresso-ip`).
- `/i/*` precisa passar pelo proxy como `/api/i/*` (SPA — 0035); o Caddy da E0c-CD deve redigir o path no access log.

## Riscos

- Diferença de tempo entre id existente e inexistente (consulta ao índice) — o custo dominante (HMAC) é o mesmo; aceitável com 256 bits de token e rate limit.

## Estratégia de rollback

Reverter o merge (sem migration). Os links já enviados voltam a não ter página.

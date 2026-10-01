# Refinamento — Épico E9 (Backoffice restante) — 2026-10-01

Cerimônia por épico (roles.md §6.14): brief único → pareceres (`arquiteto`, `security`, `qa`, `sre-devops`, depois `backend-dev`) → debate → 3 perguntas escaladas e **respondidas pelo usuário no mesmo dia**. **ADR 0011** (cancelamento como nova entrada da saga) autorizado.

## Pareceres

| Agente | Posição | Pontos-chave |
|---|---|---|
| arquiteto | seguir com ressalvas | dividir a T1 (3 assuntos); módulo `auditoria` dono da tabela; cancelamento = CAS `pago → estorno_pendente` + ingressos cancelados na hora, assentos só no `estornado`; sessão cancelada por **evento** (evitar acoplamento de TX); pivô checa sessão ativa; **cortar** cancelamento individual pelo operador; propôs 1 ADR |
| security | seguir com ressalvas | teste de tabela de authz em todas as rotas `/backoffice/*`; IDOR → 404 uniforme; convidado com e-mail + código no mesmo request (sem oráculo, rate limit da consulta); auditoria só IDs, append-only (trigger); operador vê e-mail **mascarado** e **nunca** o código do pedido (credencial do convidado) |
| qa | reformular a T1 | relógio injetado e tabela da fronteira de 2h; corridas cancelar×cancelar, ×webhook, ×job, ×sessão; `cancelavel` vem do servidor; transições inválidas em tabela; E2E sem hook de relógio (sessões semeadas perto da janela); M3/M4 intactos |
| sre-devops | reformular a T1 | cancelar sessão = TX curta sem gateway; job de estorno existente com lote/backoff; purga por `DELETE` em lotes (sem partição); índices; `cancelamentos_total{origem}`; alerta de idade do `estorno_pendente`; cenário de cancelamento no E12 |
| backend-dev | seguir com ressalvas | viável sem dependência nova; 3 tasks (~27 / ~15 / ~28 arquivos — T3 pode virar T3a/T3b); **porta + TX única** para a sessão; auditoria **na mesma TX**; trigger simples contra UPDATE/DELETE/TRUNCATE |

## Debate (divergências e resolução)

1. **Quebra** → **Consenso** (5/5): T1 grande demais. Ordem: **0036 cancelamento (fatia vertical)** → **0037 auditoria + consulta do operador** → **0038 SPA do backoffice** (dividida em 0038/0039 se passar de 30 arquivos — §6.3).
2. **Cancelar sessão com vendidos: evento × porta** → **Escalado**: usuário escolheu **porta + TX única** (padrão da porta transacional do ADR 0010).
3. **Auditoria best-effort pós-commit × mesma TX** → **Consenso** (backend, qa, sre, security × arquiteto): **mesma TX** — mutação do operador sem trilha é defeito de auditabilidade (§6.6); custo = ~8 ações passam a abrir TX.
4. **Append-only** → **Consenso**: trigger que barra UPDATE/TRUNCATE e DELETE fora da purga não é viável com um único usuário de banco → trigger contra **UPDATE e TRUNCATE**; purga por `DELETE` em lotes no worker (sem SECURITY DEFINER, sem partição — §1). Limite documentado.
5. **Cancelamento individual pelo operador** → **Escalado**: usuário escolheu **incluir** (sem janela, motivo `operador`, auditado; bloqueado para sessão já iniciada) — vai na 0037, junto com a auditoria.
6. **ADR** → **Escalado**: usuário **autorizou o ADR 0011**.
7. **Estado terminal `estorno_falhou`** (SRE) → **Consenso**: não — o ADR 0010 já define `estorno_pendente` + alerta por tentativas (§1).
8. **Rate limiter no gateway (`x/time/rate`)** (SRE) → **Consenso**: não agora — lote 10 + backoff existentes bastam; reavaliar no E12 com medição.
9. **Hook de relógio do servidor no E2E** (QA) → **Consenso**: não — sessões semeadas dentro/fora da janela; fronteira exata coberta em teste de unidade com relógio injetado.
10. **Cancelar `aguardando_pagamento`** → **Consenso**: recusado (só `pago` é cancelável; o pedido pendente expira sozinho).
11. **Fronteira das 2h** → **Consenso**: inclusiva (`agora ≤ inicio − 2h`).

## Conclusão

- 3 tasks (0036–0038; 0039 se a SPA estourar). Nenhuma dependência nova.
- Riscos: (1) corridas na máquina de estados (cancelamento × webhook × job × sessão); (2) PII vazando na auditoria/consulta do operador; (3) backoffice virar sumidouro de horas (mínimo vigiado); (4) caminho `pago → estornado` nunca exercitado — integração completa obrigatória.

## Exigências por task

### T1 (0036) — Cancelamento (cliente e sessão) + botão na SPA
- Migration: CHECK de `motivo_estorno` com `cancelamento`, `operador`, `sessao_cancelada` (up/down).
- Domínio: `Cancelar` só de `pago`; ingresso `usado` bloqueia; `agora ≤ inicio − 2h` (relógio injetado, início pela porta de sessão); tabela de transições válidas/ inválidas.
- Serviço: CAS `pago → estorno_pendente` + ingressos `cancelado` + `pedido_eventos` na mesma TX; 2º cancelamento → 200 com o estado atual.
- Rotas: logado `POST /pedidos/:id/cancelar` (Bearer, pedido alheio → 404, anti-CSRF); convidado `POST /pedidos/consulta/cancelar {email, codigo}` (resposta idêntica à consulta no "não encontrado", mesmo rate limit, comparação constante). Visão do pedido devolve `cancelavel`.
- Sessão: porta `CancelarPedidosDaSessao(ctx, tx, sessaoID)` declarada no `sessao`, implementada no `pedido`, ligada no main; `CancelarSessao` numa TX com a porta; resposta com a contagem de pedidos afetados.
- Pivô: sessão não ativa → `estorno_pendente` (`sessao_cancelada`).
- Métrica `cancelamentos_total{origem=cliente|sessao}` (`operador` na 0037).
- Testes: fronteira (2h01 / 2h00 / 1h59, sessão iniciada); corridas em PG real (cancelar×cancelar, cancelar×job, sessão×cliente, pagamento após sessão cancelada); ciclo `pago → estorno_pendente → estornado` com assentos liberados e e-mail de estorno; `/i/*` → 410 após cancelar; sessão com 0/1/N pedidos em estados mistos.
- SPA: botão "Cancelar" em "Meus pedidos" e na consulta do convidado (confirmação, segue `cancelavel`, código só em memória, erro genérico); E2E do cancelamento (logado e convidado; sessão fora da janela sem botão). Se o total passar de 30 arquivos, a SPA migra para a 0038.

### T2 (0037) — Auditoria + consulta e cancelamento do operador
- Módulo `auditoria` dono de `eventos_auditoria` (ator_id, acao enum fechado, alvo_tipo, alvo_id, ocorrido_em — **só IDs**); porta `Registrar(ctx, tx, evento)` chamada na TX de cada ação do operador (filme criar/atualizar/arquivar/importar, sala criar/atualizar, sessão criar/cancelar, pedido cancelar); trigger contra UPDATE/TRUNCATE; índices `(ocorrido_em)` e `(alvo_tipo, alvo_id, ocorrido_em)`.
- Purga diária no worker: `DELETE` em lotes de > 12 meses; métricas de removidos e última execução; teste nos limites.
- `GET /backoffice/pedidos` (filtros sessão/status, paginação com limite máx. 100, ordenação fixa) e `GET /backoffice/pedidos/:id` (e-mail **mascarado**, **sem** `codigo`, ingressos e eventos); consulta pelo próprio `pedido` (ownership).
- `POST /backoffice/pedidos/:id/cancelar`: sem janela, bloqueado se a sessão já começou, motivo `operador`, auditado na mesma TX; métrica com `origem=operador`.
- Teste de tabela de authz percorrendo **todas** as rotas `/backoffice/*` registradas (401 sem token, 403 cliente); teste de ausência de PII (e-mail/código) na tabela de auditoria e nos logs.

### T3 (0038) — SPA do backoffice mínimo
- Área `/backoffice` em chunk lazy com guarda de papel (só UX — a API decide); token em memória; CSP intacta; sem dependência nova; tokens e TanStack Query existentes; sem "admin framework".
- Filmes: lista, busca/importação TMDB, arquivar (dados externos só como texto). Salas: lista + criar/editar com template JSON em textarea (erros do servidor acessíveis; 409 com sessão ativa). Sessões: lista, criar, cancelar com confirmação mostrando os pedidos afetados. Pedidos: busca, detalhe, cancelar.
- E2E do operador (login → sala → sessão → aparece no cartaz; cancelar pedido) com axe e sem violação de CSP; TMDB fake. Divide em 0038/0039 se passar de 30 arquivos.

### Perguntas escaladas ao usuário (respondidas em 2026-10-01)

1. Cancelamento individual pelo operador → **incluir**.
2. Cancelar sessão com vendidos → **porta + TX única**.
3. ADR 0011 → **autorizado**.

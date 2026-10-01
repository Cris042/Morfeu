# PRD 0039 — SPA: sessões e pedidos do operador + E2E do backoffice (E9, T3b)

- **Task:** docs/tasks/0039-spa-sessoes-pedidos.md
- **Branch:** feature/0039-spa-sessoes-pedidos
- **Data:** 2026-10-01
- **Status:** concluído

## Objetivo

Fontes: `docs/refinamentos/E9-backoffice.md` §T3 (sessões: lista/criar/cancelar com os pedidos afetados; pedidos: busca/detalhe/cancelar; E2E do operador com axe e CSP); APIs das tasks 0013/0014, 0036 e 0037; ADR 0009.

## Escopo

Abas Sessões e Pedidos do backoffice; E2E do operador.

## Fora de escopo

- Leitura da trilha de auditoria pela UI.
- Edição de sessão (a API não tem; cancelar e programar de novo).

## Requisitos funcionais

- RF01 — Sessões: formulário com filme (só ativos), sala, início em `datetime-local` **no horário do cinema** (convertido para UTC — UTC−3 fixo) e preço em reais (→ centavos); erros `conflito_horario`, `filme_indisponivel`, `sala_inexistente`, `dados_invalidos` explicados.
- RF02 — Programação: filme, sala, dia/hora no fuso do cinema, preço, situação; "Cancelar sessão" consulta antes os pedidos **pagos** da sessão e mostra quantos serão estornados ("50 ou mais" quando a página enche); só então confirma; resposta `pedidos_estornados` exibida; `sessao_iniciada` explicada.
- RF03 — Pedidos: filtros por nº da sessão e situação; lista com e-mail mascarado (vindo da API), sessão, situação, assentos e total; paginação de 50.
- RF04 — Detalhe: situação + motivo do estorno em português, ingressos com status, histórico de estados; "Cancelar pedido" (só pago) com confirmação; `sessao_iniciada`/`nao_cancelavel` explicados.
- RF05 — E2E (`backoffice.spec.ts`, roda com `E2E_OPERADOR_EMAIL/SENHA`): operador entra, cria sala e sessão, a sessão aparece na lista pública do filme, um convidado compra D5 e o operador cancela o pedido; axe nas telas. O workflow de E2E cria o operador com `seed-operador` (senha mascarada no log).

## Requisitos não funcionais

- RNF01 — Sem dependência nova; CSS Modules com tokens; sem `style={{}}`.
- RNF02 — Nenhum dado sensível novo na SPA (o e-mail já chega mascarado; o código nunca chega).

## Critérios de aceite

- [x] CA01 — `paraUTCDoCinema("2099-10-01T20:30")` = `2099-10-01T23:30:00.000Z`.
- [x] CA02 — Programar envia filme ativo, sala, início UTC e preço em centavos; conflito explicado.
- [x] CA03 — Cancelar sessão mostra a contagem antes de chamar a API; depois mostra os estornados.
- [x] CA04 — Pedidos: filtros na URL da API, e-mail mascarado, detalhe com ingressos e histórico; cancelamento com `sessao_iniciada` explicado.
- [x] CA05 — Lint, typecheck, testes e build verdes.
- [x] CA06 — E2E do operador verde no CI; M3/M4/cancelamento intactos.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Conversão de fuso, sessões, pedidos | componente (Vitest, App inteiro) | CA01–CA04 |
| Operador ponta a ponta | E2E (Playwright, CI) | CA06 |

## Plano de implementação

1. API (`backoffice.ts`) e `paraUTCDoCinema`.
2. `Sessoes.tsx`, `Pedidos.tsx`, abas e rotas.
3. Testes; E2E + workflow.

**Skills de apoio (§4.4):** `frontend-patterns`, `e2e-testing`.

## Arquivos que serão criados

- `web/src/features/backoffice/{Sessoes.tsx, Pedidos.tsx, Operacao.test.tsx}`, `web/e2e/backoffice.spec.ts`
- `docs/tasks/0039-spa-sessoes-pedidos.md`, `docs/prd/0039-spa-sessoes-pedidos.md`

## Arquivos que serão modificados

- `web/src/api/backoffice.ts`, `web/src/ui/formato.ts`, `web/src/features/backoffice/{Backoffice.tsx, Backoffice.module.css}`, `web/e2e/m4.spec.ts`
- `.github/workflows/e2e.yml`
- `docs/tasks/README.md`, `docs/roadmap.md`, `plan.md`, `state.md`

Total: 16.

## Dependências utilizadas

Nenhuma nova.

## Impactos técnicos

- O job de E2E passa a criar um operador por execução (banco efêmero do CI).

## Riscos

- Mudança futura de fuso legal no Brasil → `paraUTCDoCinema` é o único ponto a ajustar.

## Estratégia de rollback

Reverter o merge.

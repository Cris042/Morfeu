# PRD 0020 — SPA: mapa integrado à trava (polling, holds, contagem) (E5, T4a)

- **Task:** docs/tasks/0020-spa-reserva.md
- **Branch:** feature/0020-spa-reserva
- **Data:** 2026-09-29
- **Status:** concluído

## Objetivo

Ligar o mapa (0019) à trava real do E4 na página `/sessoes/:id`:
- a ocupação é consultada por polling;
- a reserva dos assentos escolhidos só aparece depois da resposta do servidor;
- a disputa (409) é tratada na hora;
- o painel "seus assentos" traz contagem regressiva, extensão única e liberação.

Fontes: `docs/refinamentos/E5-spa-cliente.md` §T4, `docs/refinamentos/E4-reserva.md` (exigências transferidas ao E5) e ADR 0008/0009.

A T4 do refinamento foi dividida em 0020 (esta) e 0021 (E2E do M3 no CI, com o seed e a correção do teste instável do relay) para respeitar o teto de 30 arquivos (roles.md §6.3).

## Escopo

- tipos e consultas/mutations da reserva;
- página da sessão e painel de assentos;
- mensagens de erro;
- relógio para a contagem;
- ajustes não-bloqueantes da auditoria 0019;
- testes com fake timers.

## Fora de escopo

- Playwright, seed de E2E, job de E2E e correção do teste do relay (0021);
- pedido/pagamento (E6/E8);
- login (E8).

## Requisitos funcionais

- RF01 — **Tipos**: `Hold {id, sessao_id, assento, expira_em, extensoes_usadas}` e `Ocupacao {sessao_id, ocupados}`.
- RF02 — **Consultas** (`src/api/consultas.ts`):
  - `useMapa(id)` → `GET /sessoes/{id}/mapa`, com `staleTime: Infinity` (o layout não muda);
  - `useOcupacao(id)` → `GET /sessoes/{id}/ocupacao`, com **`refetchInterval: 4000`** (no meio dos 3–5 s do refinamento) e `refetchIntervalInBackground: false` (pausa com a aba fora de foco);
  - `useMeusHolds()` → `GET /holds`.
- RF03 — **Mutations**:
  - `useTravar(id)` → `POST /sessoes/{id}/holds {assentos}`;
  - `useEstender()` → `POST /holds/{id}/estender`;
  - `useLiberar()` → `DELETE /holds/{id}`;
  - todas invalidam `['holds']` e `['ocupacao', sessao]` ao terminar, com sucesso ou erro.
  - O cliente da 0017 já manda `credentials` e `X-Requested-With`.
- RF04 — **Página da sessão** (`/sessoes/:id`):
  - título "Escolha seus assentos" + sala; mapa + legenda;
  - `selecionados` em estado local;
  - `meus` = holds vivos do dono **nesta** sessão;
  - `ocupados` = a ocupação.
  - **Alternar:**
    - assento livre entra ou sai da seleção;
    - assento "meu" pede liberação.
  - Um selecionado que a ocupação passa a mostrar como de outra pessoa sai da seleção e aparece num aviso ("F7 acabou de ser reservado por outra pessoa.").
  - O botão primário "Reservar N assentos" fica desabilitado sem seleção ou durante o envio. **A UI não assume sucesso**: o assento só vira "seu" quando o `GET /holds` volta depois do 201.
  - **Erros:**
    - 409 `assento_indisponivel`: invalida a ocupação imediatamente (sem esperar o polling) e mostra os assentos recusados, removendo-os da seleção;
    - 409 `limite_holds`: "Você já tem 6 assentos reservados — o máximo por compra.";
    - 429: "Muitas tentativas seguidas. Espere um minuto e tente de novo.";
    - rede/5xx: mensagem genérica com "Tentar de novo".
  - Sessão indisponível (404 do mapa): "Esta sessão não está mais disponível" + volta ao cartaz.
- RF05 — **Painel "Seus assentos"**, para os holds vivos desta sessão:
  - cada hold mostra assento, contagem **mm:ss** a partir de `expira_em` (relógio de 1 s) e aviso visual quando faltam ≤ 60 s ("Seu assento expira em menos de um minuto");
  - botão "Mais 10 minutos", desabilitado depois da única extensão (`extensoes_usadas ≥ 1`);
  - botão "Liberar";
  - quando a contagem zera, os holds são reconsultados e o vencido some. Se o relógio local estiver defasado, quem decide é o servidor.
- RF06 — **Relógio** (`src/ui/useAgora.ts`): um intervalo de 1 s só enquanto há holds; `formato.contagem(ms)` → "09:41".
- RF07 — **Auditoria 0019** (não-bloqueantes):
  - a letra da fileira à esquerda vira `role="rowheader"`, e a da direita sai (o ARIA grid só admite células como filhas de `row`);
  - `web-ci` ganha o passo "demo fora do bundle de produção" (`grep` no `dist`).

## Requisitos não funcionais

- RNF01 — Testes com **fake timers** (`vi.useFakeTimers`, `advanceTimersByTimeAsync`) cobrindo o polling e a contagem, sem sleep real e sem rede (fetch dublê por método + caminho).
- RNF02 — Nada do carrinho no navegador além do cookie HttpOnly (lint).
- RNF03 — Mensagens com `role="alert"`/`aria-live`; foco não é roubado.

## Regras de negócio

- RN01 — O servidor é a verdade: 409 e ocupação prevalecem sobre a seleção local.
- RN02 — Seleção + holds ≤ 6 na UI; a trava (0015) garante o teto de fato.
- RN03 — TTL de 10 min com uma extensão de +10 min (ADR 0008).

## Critérios de aceite

- [x] CA01 — Ocupação reconsultada a cada 4 s (fake timers) e **não** reconsultada com a janela sem foco; mapa buscado uma única vez.
- [x] CA02 — Reservar envia `POST` com os assentos; durante o envio o botão fica desabilitado e nenhum assento aparece como "seu"; depois do 201 + `GET /holds`, os assentos aparecem como "seus".
- [x] CA03 — 409 `assento_indisponivel {assentos: ["F8"]}`:
  - ocupação reconsultada sem avançar o relógio;
  - mensagem com F8;
  - F8 sai da seleção.
- [x] CA04 — `limite_holds` e 429 com mensagens próprias.
- [x] CA05 — Painel: "09:59" que vira "09:58" após 1 s; aviso com ≤ 60 s; "Mais 10 minutos" → `POST /estender` e desabilita com `extensoes_usadas = 1`; "Liberar" → `DELETE`.
- [x] CA06 — Selecionado ocupado por outra pessoa (polling) sai da seleção com aviso.
- [x] CA07 — Sessão 404 → mensagem de indisponível.
- [x] CA08 — `rowheader` por fileira; `web-ci` verde com a verificação do bundle.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Contagem mm:ss | unit | CA05 |
| Página da sessão com fetch dublê e fake timers | componente/integração | CA01–CA07 |
| Mapa com `rowheader` | componente | CA08 |

## Plano de implementação

1. Tipos, consultas/mutations, `contagem`, `useAgora`.
2. `PaginaSessao` + `SeusAssentos` + CSS; rota.
3. Ajuste do `MapaDeAssentos` (`rowheader`); passo no `web-ci`.
4. Testes; gate; conferência visual com a API real.

**Skills de apoio (§4.4):** `frontend-patterns`.

## Arquivos que serão criados

- `web/src/features/sessao/PaginaSessao.tsx`, `PaginaSessao.module.css`, `PaginaSessao.test.tsx`, `SeusAssentos.tsx`
- `web/src/ui/useAgora.ts`
- `docs/tasks/0020-spa-reserva.md`, `docs/prd/0020-spa-reserva.md`

## Arquivos que serão modificados

- `web/src/api/tipos.ts`, `web/src/api/consultas.ts`, `web/src/ui/formato.ts`, `web/src/ui/formato.test.ts`
- `web/src/app/App.tsx`, `web/src/app/App.test.tsx`
- `web/src/features/mapa/MapaDeAssentos.tsx` (os testes da 0019 já cobrem as linhas por role=row; o rowheader é verificado na página)
- `.github/workflows/web-ci.yml`
- `docs/tasks/README.md`, `docs/tasks/0019-spa-mapa.md`, `docs/prd/0019-spa-mapa.md` (status), `plan.md`, `state.md`

Total: 20.

## Dependências utilizadas

Nenhuma nova.

## Impactos técnicos

- Primeira escrita do SPA na API: travar, estender e liberar, com cookie HttpOnly e anti-CSRF.
- Carga de polling de ~0,25 req/s por aba aberta, absorvida pelo cache de 3 s (ADR 0008).

## Riscos

- Fake timers com TanStack Query podem gerar flakiness. Mitigação: `advanceTimersByTimeAsync` e um `QueryClient` novo por teste.

## Estratégia de rollback

Reverter o merge. A rota volta ao placeholder.

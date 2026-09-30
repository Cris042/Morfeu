# PRD 0018 — SPA: cartaz e sessões do filme (E5, T2)

- **Task:** docs/tasks/0018-spa-cartaz.md
- **Branch:** feature/0018-spa-cartaz
- **Data:** 2026-09-29
- **Status:** ativo

## Objetivo

Entregar as duas primeiras telas do cliente sobre a fundação da 0017:
- o **cartaz** (`/`);
- a **página do filme** (`/filmes/:id`), com as sessões futuras.

Cada sessão leva à escolha de assentos (`/sessoes/:id`). Fonte: `docs/refinamentos/E5-spa-cliente.md` §T2. A linguagem visual segue `docs/design/identidade-visual.md` e o protótipo aprovado: topbar, grade de pôsteres, chips em mono e pôster tipográfico.

## Escopo

Tipos da API, consultas (TanStack Query), formatação pt-BR/fuso, componentes de pôster e de estado, as duas páginas, layout do shell, rota placeholder de sessão, testes.

## Fora de escopo

- mapa (0019);
- holds, polling e E2E (0020);
- classificação indicativa, porque o catálogo ainda não tem o campo — registrado para quando existir;
- busca/filtros;
- anti-stampede do cartaz (E12).

## Requisitos funcionais

- RF01 — **Tipos** (`src/api/tipos.ts`), espelho do contrato público:
  - `Filme {id, titulo, sinopse?, duracao_min?, ano?, poster_url?}`;
  - `SessaoPublica {id, sala_id, sala_nome, inicio, fim, preco_centavos}` (instantes em ISO UTC).
- RF02 — **Consultas** (`src/api/consultas.ts`) pelo cliente único (política de retry global no `QueryClient`: só rede/5xx, 1 vez):
  - `useFilmes()` → `GET /filmes`;
  - `useFilme(id)` → `GET /filmes/{id}`;
  - `useSessoesDoFilme(id)` → `GET /filmes/{id}/sessoes`;
  - chaves `['filmes']`, `['filme', id]` e `['sessoes-do-filme', id]`;
  - 404 não é repetido (retry só para rede/5xx).
- RF03 — **Formatação** (`src/ui/formato.ts`):
  - sempre `timeZone: 'America/Sao_Paulo'` e locale `pt-BR`: `hora(iso)` → "20:30"; `dia(iso)` → "qua., 1 de out."; `chaveDia(iso)` para agrupar;
  - `preco(centavos)` → "R$ 32,00";
  - `duracao(min)` → "2h 22min".
- RF04 — **Pôster** (`src/ui/Poster.tsx`):
  - `<img>` só quando `poster_url` é `https://image.tmdb.org/...` (a CSP da 0017 só permite esse host), com `alt` = título e `loading="lazy"`;
  - caso contrário, pôster tipográfico: título em Fraunces sobre um gradiente escuro derivado do id (determinístico) — vale também para os pôsteres de exemplo do seed.
- RF05 — **Estados** (`src/ui/Estado.tsx`):
  - carregando (texto `role="status"`);
  - erro com ação "Tentar de novo" (refetch);
  - vazio com texto próprio de cada tela.
  - Microcopy na voz da bilheteria: direta, sem jargão.
- RF06 — **Cartaz** (`/`):
  - eyebrow "Em cartaz" + título;
  - grade responsiva (4 → 2 → 1 colunas) de cartões com pôster, título (h2 dentro do card) e meta mono (ano · duração);
  - o cartão é um link para `/filmes/:id`;
  - vazio: "Nenhum filme em cartaz agora. Volte mais tarde.".
- RF07 — **Página do filme** (`/filmes/:id`):
  - pôster, título (h1), meta, sinopse (texto puro, ≤ 65ch);
  - "Sessões" agrupadas por dia (rótulo mono), cada sessão como chip-link mono "20:30 · Sala 1 · R$ 32,00" para `/sessoes/:id`;
  - sem sessões: "Sem sessões programadas para este filme.";
  - 404 (inexistente/arquivado, inclusive o cache de ≤ 60 s da auditoria 0014) → "Este filme não está em cartaz" com volta ao cartaz;
  - id não numérico → mesmo 404, sem chamar a API.
- RF08 — **Shell**:
  - topbar com a marca (link para `/`) e rodapé com a atribuição do TMDB, estilizados com CSS Modules;
  - rotas `/`, `/filmes/:id`, `/sessoes/:id` (placeholder "A escolha de assentos abre em breve." até a 0020) e `*`.

## Requisitos não funcionais

- RNF01 — Título/sinopse/sala só como texto (o lint da 0017 barra HTML cru).
- RNF02 — Testes sem rede (fetch dublê) e com relógio/fuso explícitos. Nenhum teste depende do fuso da máquina.
- RNF03 — Acessibilidade:
  - hierarquia de títulos;
  - links com nome acessível;
  - `alt` no pôster (tipográfico: `role="img"` + `aria-label`);
  - foco visível pelo token.
- RNF04 — Sem dependência nova.

## Regras de negócio

- RN01 — O cliente não refiltra sessões: a API já devolve só as futuras e agendadas.
- RN02 — O preço vem do servidor em centavos; o cliente só formata.

## Critérios de aceite

- [ ] CA01 — `formato.test.ts`:
  - 23:30 UTC vira "20:30" do dia local;
  - 02:00 UTC vira o dia anterior em São Paulo;
  - 3200 → "R$ 32,00";
  - 142 → "2h 22min".
- [ ] CA02 — Cartaz: lista, link para o detalhe, vazio, erro com "Tentar de novo" (refaz a consulta).
- [ ] CA03 — Pôster: `image.tmdb.org` → `<img alt>`; `example.com`/ausente → tipográfico com `aria-label`.
- [ ] CA04 — Filme: dados + sessões agrupadas por dia com hora local e preço; sem sessões; 404 → "não está em cartaz"; id inválido sem chamada à API.
- [ ] CA05 — Sinopse com `<script>` aparece como texto, sem virar elemento.
- [ ] CA06 — lint/typecheck/test/build/audit verdes; `web-ci` verde.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Formatação com fuso fixo | unit | CA01 |
| Cartaz, pôster, filme com fetch dublê | componente (Testing Library) | CA02–CA05 |

## Plano de implementação

1. `tipos.ts`, `consultas.ts`, `formato.ts` + testes.
2. `Poster`, `Estado`.
3. `Cartaz`, `PaginaFilme` + CSS Modules + testes.
4. Shell (`App` + CSS), rotas, helper de teste.
5. Gate local.

**Skills de apoio (§4.4):** `frontend-patterns`, `design-system`.

## Arquivos que serão criados

- `web/src/api/tipos.ts`, `web/src/api/consultas.ts`
- `web/src/ui/formato.ts`, `web/src/ui/formato.test.ts`, `web/src/ui/Poster.tsx`, `web/src/ui/Poster.module.css`, `web/src/ui/Estado.tsx`, `web/src/ui/Estado.module.css`
- `web/src/features/cartaz/Cartaz.tsx`, `Cartaz.module.css`, `Cartaz.test.tsx`
- `web/src/features/filme/PaginaFilme.tsx`, `PaginaFilme.module.css`, `PaginaFilme.test.tsx`
- `web/src/app/App.module.css`, `web/src/test/renderizar.tsx`
- `docs/tasks/0018-spa-cartaz.md`, `docs/prd/0018-spa-cartaz.md`

## Arquivos que serão modificados

- `web/src/app/App.tsx`, `web/src/app/App.test.tsx`, `web/src/main.tsx` (política de retry global)
- `docs/tasks/README.md`, `docs/tasks/0017-spa-fundacao.md`, `docs/prd/0017-spa-fundacao.md` (status), `plan.md`, `state.md`

Total: 26.

## Dependências utilizadas

Nenhuma nova.

## Impactos técnicos

Primeiras telas reais do cliente, sem impacto no backend.

## Riscos

- Diferença de ICU entre Node local e o CI na formatação de datas. Mitigação: Node 24 com ICU completo nos dois; os testes comparam com `Intl` quando o texto exato depende de ICU (dia da semana abreviado).

## Estratégia de rollback

Reverter o merge.

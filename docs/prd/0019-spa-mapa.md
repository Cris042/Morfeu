# PRD 0019 — SPA: mapa de assentos isolado (dados fake) (E5, T3)

- **Task:** docs/tasks/0019-spa-mapa.md
- **Branch:** feature/0019-spa-mapa
- **Data:** 2026-09-29
- **Status:** ativo

## Objetivo

Entregar o componente do mapa de assentos, peça central da demo M3, como componente **puro** (sem I/O). Ele recebe o layout e os estados e emite a intenção de alternar um assento. É acessível por teclado e leitor de tela, e cada estado se distingue sem depender de cor.

A integração com a trava real (polling, holds, 409) fica para a 0020. Fontes:
- `docs/refinamentos/E5-spa-cliente.md` §T3;
- risco nº 3 do roadmap (grade simples, nunca editor visual);
- a identidade visual.

## Escopo

Tipos do mapa; função pura de estado; componente `MapaDeAssentos`; legenda; fixture; página de demonstração só em desenvolvimento.

## Fora de escopo

Rede, polling, holds, contagem regressiva, extensão, E2E (0020); zoom/arrastar; editor de layout (nunca).

## Requisitos funcionais

- RF01 — **Tipos** (`src/api/tipos.ts`), espelho de `GET /sessoes/{id}/mapa`: `Assento {codigo, fileira, coluna, pcd}` e `MapaSessao {sessao_id, sala_id, sala_nome, fileiras, colunas, assentos}`. Vão = posição da grade sem assento.
- RF02 — **Estado do assento** (`src/features/mapa/estado.ts`, função pura):
  - `estadoDoAssento(codigo, {ocupados, meus, selecionados, limiteAtingido})` → `'meu' | 'selecionado' | 'ocupado' | 'bloqueado' | 'livre'`;
  - precedência: meu > selecionado > ocupado > bloqueado (limite) > livre;
  - um assento meu também aparece em `ocupados` (a ocupação pública não distingue dono), e prevalece "meu";
  - `rotuloDoAssento` monta o `aria-label` completo: "Fileira C, assento 7, PCD, ocupado".
- RF03 — **`MapaDeAssentos`** (`src/features/mapa/MapaDeAssentos.tsx`):
  - props `{ mapa, ocupados, meus, selecionados, maximo = 6, onAlternar(codigo) }`;
  - sem estado de dados próprio (só o foco).
  - **Estrutura:**
    - "Tela" no topo;
    - `role="grid"` com `aria-label` "Assentos da {sala}";
    - uma `role="row"` por fileira, com a letra da fileira nas duas pontas (`aria-hidden`);
    - uma `role="gridcell"` por posição;
    - assento = `<button>` dentro da célula; vão = célula vazia, fora do foco.
  - **Teclado (roving tabindex):**
    - só um botão tem `tabIndex=0` (o último focado; no início, o primeiro assento livre ou o primeiro assento);
    - setas movem ao próximo assento na direção, pulando vãos (←/→ na fileira; ↑/↓ na mesma coluna, ou no assento mais próximo da fileira vizinha);
    - Home/End vão ao primeiro/último assento da fileira;
    - Enter/Espaço alternam (clique nativo do botão).
  - **Estados:**
    - livre: selecionável;
    - selecionado: `aria-pressed="true"`, fundo tungstênio + marca ✓;
    - meu (hold do usuário): marca ●, `aria-pressed="true"`, alternar pede liberação (a 0020 decide);
    - ocupado: `aria-disabled="true"` + marca ×, não alterna;
    - bloqueado (limite atingido): `aria-disabled="true"`, não alterna;
    - PCD: marca ♿ e "PCD" no rótulo.
    - As marcas são `aria-hidden`; o rótulo carrega o significado.
  - **Limite:** selecionados + meus ≥ `maximo` bloqueia os livres. Uma mensagem viva (`aria-live="polite"`) informa "Você já escolheu 6 assentos — o máximo por compra.".
  - Usa `aria-disabled` (não `disabled`) para o assento continuar focável e anunciado.
- RF04 — **Legenda** (`Legenda.tsx`): livre, selecionado, seu, ocupado, PCD, com as mesmas marcas.
- RF05 — **Fixture** (`src/features/mapa/fixture.ts`): sala 6 × 10 com corredor (vãos na coluna 5), fileira F mais curta (vãos nas pontas), PCD em A1/A2 e ocupados de exemplo.
- RF06 — **Demonstração** `/_demo/mapa`, registrada só com `import.meta.env.DEV` e removida do build de produção. Usa a fixture com estado local, para conferência visual e manual antes da 0020.

## Requisitos não funcionais

- RNF01 — Acessibilidade: padrão ARIA grid (APG), foco visível (halo tungstênio), alvos ≥ 32 px, `prefers-reduced-motion` já global.
- RNF02 — Desempenho: a grade máxima (26 × 50 = 1300 células) renderiza sem estado por célula; o foco é um único estado.
- RNF03 — Testes com Testing Library + user-event, sem rede e sem timers reais.

## Regras de negócio

- RN01 — Máximo de 6 assentos por compra (a trava da 0015 limita holds vivos por dono a 6). A UI espelha o limite; o servidor segue sendo a verdade.
- RN02 — Ocupado e vão nunca são selecionáveis.

## Critérios de aceite

- [ ] CA01 — `estado.test.ts`: precedência, meu dentro de ocupados, limite e rótulo completo.
- [ ] CA02 — Grade fiel: 6 fileiras, vãos sem botão, 1 tab stop.
- [ ] CA03 — Teclado:
  - Tab entra no primeiro assento;
  - → pula o corredor;
  - ↓ vai à fileira de baixo na mesma coluna ou no assento mais próximo;
  - Home/End funcionam;
  - Espaço/Enter chamam `onAlternar`;
  - o foco segue com roving tabindex.
- [ ] CA04 — Ocupado e bloqueado não chamam `onAlternar`; selecionado tem `aria-pressed`; PCD no rótulo; aviso de limite visível ao atingir 6.
- [ ] CA05 — Demonstração fora do build de produção (a rota não existe com `DEV=false`, verificado pelo bundle).
- [ ] CA06 — lint/typecheck/test/build/audit; `web-ci` verde.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Estado e rótulo | unit | CA01 |
| Grade, teclado, estados, limite | componente (user-event) | CA02–CA04 |
| Bundle de produção sem a demonstração | build + grep | CA05 |

## Plano de implementação

1. Tipos + `estado.ts` + testes.
2. `MapaDeAssentos` + CSS + `Legenda`.
3. Testes de componente com teclado.
4. Fixture + demonstração em dev.
5. Gate; conferência visual.

**Skills de apoio (§4.4):** `frontend-patterns`, `design-system`.

## Arquivos que serão criados

- `web/src/features/mapa/estado.ts`, `estado.test.ts`, `MapaDeAssentos.tsx`, `MapaDeAssentos.module.css`, `MapaDeAssentos.test.tsx`, `Legenda.tsx`, `fixture.ts`, `DemoMapa.tsx`
- `docs/tasks/0019-spa-mapa.md`, `docs/prd/0019-spa-mapa.md`

## Arquivos que serão modificados

- `web/src/api/tipos.ts`, `web/src/app/App.tsx` (rota de demonstração em dev)
- `docs/tasks/README.md`, `docs/tasks/0018-spa-cartaz.md`, `docs/prd/0018-spa-cartaz.md` (status), `plan.md`, `state.md`

Total previsto: 17.

## Dependências utilizadas

Nenhuma nova.

## Impactos técnicos

Nenhum no backend. A rota de demonstração existe só no servidor de desenvolvimento.

## Riscos

- Navegação vertical com vãos irregulares confusa. Mitigação: regra simples e testada (mesma coluna; senão, o assento mais próximo na fileira vizinha).

## Estratégia de rollback

Reverter o merge.

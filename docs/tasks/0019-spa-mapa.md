# Task 0019 — SPA: mapa de assentos isolado (dados fake) (E5, T3)

- **Data:** 2026-09-29
- **Status:** concluída
- **Branch:** `feature/0019-spa-mapa` (da main `849064b`)
- **PRD:** docs/prd/0019-spa-mapa.md
- **Item do roadmap:** E5 — SPA cliente, parte 1 (task 3/4). Refinamento: `docs/refinamentos/E5-spa-cliente.md` §T3.

## Objetivo

Construir o componente central do M3 — o mapa de assentos — como componente puro (sem I/O), acessível por teclado, validado com dados fake antes de ligar na trava real (0020). Risco nº 3 do roadmap: grade simples do template JSON, nunca editor visual.

## Escopo

Tipos do mapa; função pura de estado do assento; componente `MapaDeAssentos` (grid ARIA, roving tabindex, setas/Home/End, Enter/Espaço, estados distinguíveis sem cor, limite de 6); legenda; fixture; página de demonstração só em dev (`/_demo/mapa`).

## Fora de escopo

Rede, polling, holds, contagem regressiva, E2E (0020).

## Arquivos esperados

~17 (lista no PRD).

## Dependências esperadas

Nenhuma nova.

## Critérios de aceite

- [x] Grade fiel ao layout (fileiras × colunas, vãos vazios e fora do foco, PCD marcado).
- [x] Teclado: Tab único entra na grade; setas movem entre assentos pulando vãos; Home/End; Enter/Espaço alternam.
- [x] `aria-label` completo por assento; `aria-pressed` no selecionado; ocupado/meu/limite com `aria-disabled`; estados distinguíveis sem cor.
- [x] Limite de 6 (selecionados + meus) bloqueia novos assentos com aviso.
- [x] Testes com user-event; `web-ci` verde.

## Riscos

- Grade grande (26 × 50) lenta → componente sem estado interno pesado; render simples.

## Estimativa de impacto

Médio no front; nenhum no backend.

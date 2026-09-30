# Task 0018 — SPA: cartaz e sessões do filme (E5, T2)

- **Data:** 2026-09-29
- **Status:** concluída
- **Branch:** `feature/0018-spa-cartaz` (da main `cce783c`)
- **PRD:** docs/prd/0018-spa-cartaz.md
- **Item do roadmap:** E5 — SPA cliente, parte 1 (task 2/4). Refinamento: `docs/refinamentos/E5-spa-cliente.md` §T2.

## Objetivo

Primeiras telas reais do cliente: o cartaz (`/`) e a página do filme (`/filmes/:id`) com as sessões futuras, cada uma levando à escolha de assentos (`/sessoes/:id`, preenchida nas tasks 0019/0020).

## Escopo

Consultas com TanStack Query pelo cliente único; cartaz em grade; página do filme com sessões agrupadas por dia; pôster TMDB ou tipográfico; horário em America/Sao_Paulo e preço em BRL; estados carregando/vazio/erro/404; layout do shell.

## Fora de escopo

Mapa de assentos (0019); holds/polling/E2E (0020); classificação indicativa (o catálogo ainda não tem o campo); busca/filtros.

## Arquivos esperados

~23 (lista no PRD).

## Dependências esperadas

Nenhuma nova.

## Critérios de aceite

- [ ] Cartaz lista os filmes com link para o detalhe; vazio e erro com mensagem clara.
- [ ] Detalhe mostra título/sinopse como texto, sessões por dia com hora local, sala e preço em BRL; filme inexistente/arquivado → "não encontrado".
- [ ] Pôster só de `image.tmdb.org`; outros → pôster tipográfico.
- [ ] Testes de componente sem rede; formatação testada com fuso fixo.
- [ ] `web-ci` verde.

## Riscos

- Fuso da máquina de CI → formatação sempre com `timeZone` explícito.

## Estimativa de impacto

Médio no front; nenhum no backend.

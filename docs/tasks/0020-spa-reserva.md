# Task 0020 — SPA: mapa integrado à trava (polling, holds, contagem) (E5, T4a)

- **Data:** 2026-09-29
- **Status:** concluída
- **Branch:** `feature/0020-spa-reserva` (da main `0117c41`)
- **PRD:** docs/prd/0020-spa-reserva.md
- **Item do roadmap:** E5 — SPA cliente, parte 1 (T4 dividida: 0020 integração + 0021 E2E do M3 — o conjunto passava de 30 arquivos). Refinamento: `docs/refinamentos/E5-spa-cliente.md` §T4.

## Objetivo

Ligar o mapa de assentos (0019) à trava real (E4): ocupação por polling, reservar os assentos escolhidos, tratar a disputa (409) na hora, e o painel "seus assentos" com contagem regressiva, extensão única e liberação.

## Escopo

Página `/sessoes/:id`; consultas/mutations (mapa, ocupação com polling pausado em background, holds, travar, estender, liberar); mensagens de 409/limite/429; contagem por `expira_em`; ajustes não-bloqueantes da auditoria 0019 (letra da fileira como `rowheader`; `web-ci` confere que a demo não vai para produção).

## Fora de escopo

E2E Playwright do M3 + job de CI + correção do teste instável do relay (0021); pagamento/pedido (E6/E8).

## Arquivos esperados

~22 (lista no PRD).

## Dependências esperadas

Nenhuma nova.

## Critérios de aceite

- [x] Ocupação atualizada a cada 4 s, pausada com a aba fora de foco; mapa sem refetch.
- [x] Reservar: a UI só mostra "seu" depois da resposta 201; 409 invalida a ocupação na hora e diz quais assentos foram perdidos; limite e 429 com mensagem própria.
- [x] Painel: contagem mm:ss por `expira_em`, aviso ≤ 60 s, "Mais 10 minutos" uma vez, "Liberar"; hold vencido some.
- [x] Testes com fake timers; `web-ci` verde (com a verificação da demo fora do bundle).

## Riscos

- Relógio do cliente adiantado/atrasado → contagem é aviso; o servidor decide (404/409).

## Estimativa de impacto

Médio no front; nenhum no backend.

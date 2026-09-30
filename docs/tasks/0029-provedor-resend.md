# Task 0029 — Provedor Resend, config, métricas e alertas de e-mail (E7, T2)

- **Data:** 2026-09-30
- **Status:** em andamento
- **Branch:** `feature/0029-provedor-resend` (da main `1088c66`)
- **PRD:** docs/prd/0029-provedor-resend.md
- **Item do roadmap:** E7 — Notificação (2ª de 3). Refinamento: `docs/refinamentos/E7-notificacao.md` §T2.

## Objetivo

Ligar o envio real do e-mail do ingresso pelo Resend (HTTP cru), com a classificação de erros que decide DLQ × redelivery, a config com recusas de boot, métricas por resultado e dois alertas.

## Escopo

Adapter `notificacao.Resend`; classes `ErrEnvioPermanente`/`ErrCotaEsgotada`; resultado de cada entrega no consumidor; config `EMAIL_PROVEDOR`/`RESEND_API_KEY`/`EMAIL_REMETENTE` (fake proibido em produção); métricas `morfeu_email_*`; 2 alertas; regra gitleaks `re_`.

## Fora de escopo

E-mail de estorno (0030); domínio verificado (deploy — decisão do usuário: demo só para o dono da conta até lá).

## Arquivos esperados

19 (lista no PRD).

## Dependências esperadas

Nenhuma nova (HTTP cru, stdlib).

## Critérios de aceite

- [ ] POST /emails com Authorization, Idempotency-Key e QRs inline.
- [ ] Cota → DLQ + `resultado=cota`; recusa → DLQ; taxa/5xx/timeout → redelivery; erro sem o corpo da resposta.
- [ ] Boot recusa fake em produção e Resend sem chave/remetente.
- [ ] 16 alertas provisionados; labels `provedor`/`tipo` no /metrics.

## Riscos

- Sem domínio verificado o Resend só entrega ao dono da conta (aceito pelo usuário).

## Estimativa de impacto

Baixo: troca do sender por config; nenhuma rota nova.

# Task 0028 — Notificação: núcleo do e-mail de confirmação com QR (E7, T1)

- **Data:** 2026-09-30
- **Status:** em andamento
- **Branch:** `feature/0028-notificacao-nucleo` (da main `43a84fd`)
- **PRD:** docs/prd/0028-notificacao-nucleo.md
- **Item do roadmap:** E7 — Notificação: e-mail + QR (1ª de 3). Refinamento: `docs/refinamentos/E7-notificacao.md` §T1; ADR 0005 (Strategy) e 0010 (token HMAC).

## Objetivo

Transformar o consumidor stub de `pedido.confirmado` na entrega real do ingresso: carregar o pedido pago pelas portas de pedido/sessão/catálogo, calcular o token HMAC de cada ingresso, gerar um QR por ingresso e montar o e-mail (HTML + texto) — enviado por um `EmailSender` (fake nesta task; Resend na 0029).

## Escopo

Token `HMAC(seg_v, "ingresso:v1:"+id)` no `pedido`; portas `DadosParaNotificacao` (pedido), `DadosParaIngresso` (sessão), `TituloDoFilme` (catálogo); adapter `FonteDoEmail` no main; `EmailSender` + fake; template `html/template` + texto; QR PNG inline por CID; config do segredo e da URL pública; libs `skip2/go-qrcode` e `gozxing` (teste).

## Fora de escopo

Provedor real, métricas/alertas de envio (0029); e-mail de estorno (0030); página pública `/i/{id}.{token}` (E8).

## Arquivos esperados

28 (lista no PRD).

## Dependências esperadas

Novas: `github.com/skip2/go-qrcode` (runtime) e `github.com/makiuchi-d/gozxing` (só teste) — registradas no `lib.md`.

## Critérios de aceite

- [ ] Só pedido pago com ingresso ativo gera e-mail; token determinístico por segredo.
- [ ] Um QR por ingresso, decodificável para `/i/{id}.{token}`.
- [ ] HTML com escape, sem imagem externa, links só da base configurada; golden estável.
- [ ] Não notificável = ack sem envio; inexistente = DLQ.

## Riscos

- `html/template` bloqueia `cid:` vindo de dado → prefixo literal no template (coberto por teste).

## Estimativa de impacto

Médio: o worker passa a montar e-mails; nenhuma rota nova.

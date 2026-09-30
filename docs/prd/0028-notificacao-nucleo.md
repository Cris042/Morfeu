# PRD 0028 — Notificação: núcleo do e-mail de confirmação com QR (E7, T1)

- **Task:** docs/tasks/0028-notificacao-nucleo.md
- **Branch:** feature/0028-notificacao-nucleo
- **Data:** 2026-09-30
- **Status:** concluído

## Objetivo

Entregar o ingresso ao cliente depois do pivô (ADR 0010, pós-pivô). O consumidor de `pedido.confirmado` carrega o pedido pago pelas portas dos módulos e calcula o token de cada ingresso no `pedido`. Depois gera um QR por ingresso e monta o e-mail, que é enviado pela porta `EmailSender`: fake nesta task, Resend na 0029. Fontes: `docs/refinamentos/E7-notificacao.md` §T1, ADR 0003/0005/0010.

## Escopo

Token HMAC, três portas de leitura, adapter `FonteDoEmail`, `EmailSender` + fake, templates, QR, config do segredo e da URL pública, libs novas e testes.

## Fora de escopo

- Provedor Resend, config do provedor, métricas `morfeu_email_*` e alertas (0029).
- E-mail de estorno (0030).
- Página pública `/i/{id}.{token}` com expiração e revogação (E8).
- Reenvio sob demanda (E8/E9).

## Requisitos funcionais

- RF01 — `pedido.TokenIngresso(segredo, id)` = HMAC-SHA256(segredo, `"ingresso:v1:"+id`) em base64url sem padding (43 caracteres, 256 bits). Nada em claro no banco; o segredo da versão vem de `Config.SegredosToken[versao_token]` e nunca sai do módulo.
- RF02 — Porta `pedido.DadosParaNotificacao(id)` → e-mail, código, sessão, total e ingressos **ativos** com token.
  - Inexistente → `ErrPedidoNaoEncontrado`.
  - Não `pago` ou sem ingresso ativo → `ErrNaoNotificavel`.
- RF03 — Portas de leitura sem filtro de "aberta/arquivado", porque a sessão pode ter começado e o filme pode ter saído do cartaz depois da compra:
  - `sessao.DadosParaIngresso(id)` → filme, início, sala.
  - `catalogo.TituloDoFilme(id)`.
- RF04 — Adapter `fonteDoEmail` no main compõe as três portas e traduz os erros para `notificacao.ErrNaoNotificavel`/`ErrPedidoInexistente`. Nenhum módulo importa outro (ADR 0003).
- RF05 — `notificacao.Entregador`: carregar → montar → enviar.
  - Consumidor: `ErrNaoNotificavel` → ack sem envio e sem medir latência.
  - `ErrPedidoInexistente` → permanente (DLQ).
  - Falha do `EmailSender` → transitória (redelivery → DLQ).
- RF06 — E-mail de confirmação:
  - Assunto fixo "Seus ingressos — Morfeu", sem dado livre.
  - HTML (`html/template`, escape automático, nenhum `template.HTML`) + texto puro.
  - Filme, data/hora em `America/Sao_Paulo` (`time/tzdata` embutido, porque a imagem é scratch), sala, código do pedido e total em BRL.
  - Por ingresso: assento, QR PNG 256 px inline por `cid:ingresso-{assento}` e link `{BASE_URL_PUBLICA}/i/{id}.{token}`.
  - Nenhuma imagem externa, nenhum pixel de rastreamento.
  - O prefixo `cid:` é literal no template, porque o `html/template` bloqueia esquema não-http vindo de dado.
- RF07 — `Mensagem.ChaveIdempotencia` = `confirmacao-{pedido_id}` (usada pelo provedor na 0029).
- RF08 — Fake do `EmailSender`: grava as mensagens, com falha programável. Nesta task é o único sender ligado no main; o boot com provedor real e a recusa do fake em produção são da 0029.
- RF09 — Config:
  - `INGRESSO_TOKEN_SEGREDO_V1`: ≥ 32 bytes quando definido; obrigatório em produção.
  - `BASE_URL_PUBLICA`: padrão `http://localhost:5173`; https obrigatório em produção.
  - Em dev sem segredo, o main gera um aleatório por processo, com aviso.
- RF10 — O consumidor de `pedido.confirmado` passa a subir depois dos serviços de domínio, porque depende das portas.

## Requisitos não funcionais

- RNF01 — Sem e-mail, token ou link em logs: só `pedido_id`.
- RNF02 — Depguard: a `notificacao` só importa `outbox`, uuid, zap e o gerador de QR.
- RNF03 — Testes sem rede. O QR é **decodificado** no teste (`gozxing`) e o HTML/texto é conferido por golden.

## Regras de negócio

- RN01 — Nunca sai QR de ingresso inválido: só pedido `pago` e ingressos `ativo`.
- RN02 — O e-mail é pós-pivô: a falha dele nunca toca a venda (já provado pelo teste ponta a ponta da 0026).

## Critérios de aceite

- [x] CA01 — Porta do pedido: aguardando → não notificável; inexistente → não encontrado; pago → 2 ingressos com token = `TokenIngresso(segredo, id)`, 43 caracteres, sem o id em claro, dependente do segredo, distinto entre ingressos; ingressos cancelados → não notificável.
- [x] CA02 — Um QR por ingresso, `image/png`, `cid:ingresso-{assento}` referenciado no HTML e decodificado para `https://…/i/{id}.{token}`.
- [x] CA03 — Golden do HTML + texto (20:30 em Brasília para 23:30 UTC; R$ 1.234,56), assunto fixo, chave de idempotência, destinatário.
- [x] CA04 — Título hostil escapado; toda `src` começa com `cid:`; todo `href` começa com a base; base não http(s) recusada.
- [x] CA05 — `Entregar`: envia 1; não notificável e inexistente não enviam; falha do sender sobe.
- [x] CA06 — Consumidor: não notificável = ack sem latência; inexistente = permanente.
- [x] CA07 — Config: segredo curto recusado; produção sem segredo ou com base http recusada.
- [x] CA08 — Suíte completa e CI verdes.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Porta do pedido e token com PG real (via webhook do pivô) | integração (`pedido`) | CA01 |
| Montagem, QR decodificado, golden, segurança, entregador, consumidor | unit (`notificacao`) | CA02–CA06 |
| Recusas de config | unit (`config`) | CA07 |

## Plano de implementação

1. Queries e portas em `catalogo`, `sessao` e `pedido` (+ token).
2. `notificacao/email.go`: tipos, entregador, templates, QR, fake.
3. Consumidor e `main` (adapter, ordem de inicialização), config.
4. Libs (`go doc` + OSV + `lib.md`), depguard, testes, golden.

**Skills de apoio (§4.4):** `golang-testing`, `golang-error-handling`.

## Arquivos que serão criados

- `internal/pedido/notificacao.go`, `internal/notificacao/email.go`, `internal/notificacao/testdata/confirmacao.golden`
- `docs/tasks/0028-notificacao-nucleo.md`, `docs/prd/0028-notificacao-nucleo.md`

## Arquivos que serão modificados

- `internal/catalogo/{queries.sql, service.go}`, `internal/catalogo/db/{queries.sql.go, querier.go}` (gerados)
- `internal/sessao/{queries.sql, service.go}`, `internal/sessao/db/queries.sql.go` (gerado)
- `internal/pedido/{queries.sql, service.go, handler_test.go, pivo_test.go}`, `internal/pedido/db/queries.sql.go` (gerado)
- `internal/notificacao/{consumidor.go, consumidor_test.go}`
- `internal/config/{config.go, config_test.go}`, `cmd/morfeu/main.go`
- `go.mod`, `go.sum`, `lib.md`, `.golangci.yml`
- `docs/tasks/README.md`, `plan.md`, `state.md`

Total: 30 (+ `docs/ambiente-dev.md` no fechamento, nota da auditoria; `internal/notificacao/testdata/confirmacao.golden`). O roadmap é atualizado no fechamento do E7.

## Dependências utilizadas

`skip2/go-qrcode` (runtime, zero dependências) e `makiuchi-d/gozxing` (só teste). OSV sem advisories em 2026-09-30. O Context7 não indexa nenhuma das duas; a API foi verificada por `go doc` no módulo baixado.

## Impactos técnicos

- O worker passa a montar e-mails (CPU desprezível: um PNG de 256 px por ingresso).
- `time/tzdata` embutido (+~450 KB no binário).

## Riscos

- Link aponta para a página que só existe no E8. O QR e o código do pedido já servem no balcão.

## Estratégia de rollback

Reverter o merge. O consumidor volta ao stub; nenhuma migration.

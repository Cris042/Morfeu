# PRD 0029 — Provedor Resend, config, métricas e alertas de e-mail (E7, T2)

- **Task:** docs/tasks/0029-provedor-resend.md
- **Branch:** feature/0029-provedor-resend
- **Data:** 2026-09-30
- **Status:** concluído

## Objetivo

Enviar de verdade o e-mail do ingresso montado na 0028. O envio usa o Resend, o único provedor com imagem inline por CID e `Idempotency-Key` (confirmado no Context7 no refinamento E7). A classificação de erros decide DLQ ou redelivery, e métricas e alertas tornam a entrega observável. Fontes: `docs/refinamentos/E7-notificacao.md` §T2, auditoria 0028 (N2: fake proibido em produção).

## Escopo

Adapter por HTTP cru, classes de erro, resultado por entrega, config e boot, métricas, alertas, gitleaks, `.env.example` e testes.

## Fora de escopo

- E-mail de estorno (0030).
- Verificação de domínio (SPF/DKIM/DMARC): até lá, o Resend só entrega ao dono da conta (decisão do usuário).
- Circuit breaker: só no gateway de pagamento (doc.md §13).

## Requisitos funcionais

- RF01 — `Resend.Enviar` faz `POST {URL}/emails` com:
  - Headers: `Authorization: Bearer`, `Content-Type: application/json` e `Idempotency-Key` = `Mensagem.ChaveIdempotencia` (`confirmacao-{pedido_id}`; o Resend descarta repetição por 24 h).
  - Corpo: `from`, `to`, `subject`, `html`, `text` e `attachments[{filename, content base64, content_id, content_type}]`.
  - Timeout de 5 s. Sem retry interno, porque a redelivery do consumidor já repete. Resposta lida com limite de 64 KB.
- RF02 — Classificação (Context7, API de erros do Resend):

  | Resposta | Classe | Destino |
  |---|---|---|
  | 2xx | ok | — |
  | 429 `daily_quota_exceeded`/`monthly_quota_exceeded` | `ErrCotaEsgotada` | DLQ; replay no dia seguinte |
  | 429 de taxa, 5xx, 409 `concurrent_idempotent_requests`, rede/timeout | transitório | redelivery |
  | demais 4xx (422, 403, 409 `invalid_idempotent_request`…) | `ErrEnvioPermanente` | DLQ |

  O erro carrega só o status e o `name`, nunca o corpo da resposta, que pode ecoar o destinatário.
- RF03 — O consumidor registra cada entrega em `Config.Resultado(tipo, resultado, duração)`, com os resultados `ok`, `ignorado`, `transitorio`, `permanente` e `cota`. Pedido inexistente, recusa e cota são permanentes (DLQ); não notificável é ack.
- RF04 — Config:
  - `EMAIL_PROVEDOR` = `fake` (padrão) ou `resend`.
  - `RESEND_API_KEY`: `re_…`, obrigatória com `resend`.
  - `EMAIL_REMETENTE`: padrão `Morfeu <onboarding@resend.dev>`.
  - **O boot recusa `fake` com `AMBIENTE=producao`** (auditoria 0028 N2) e provedor desconhecido.
- RF05 — Métricas:
  - `morfeu_email_envios_total{provedor,tipo,resultado}`.
  - `morfeu_email_envio_duracao_segundos{provedor}` (sem `WithUnit`; buckets de 0,1 a 10 s).
  - `provedor` e `tipo` entram na allowlist da telemetria, lição da 0026.
- RF06 — Alertas (16 regras):
  - "E-mail recusado ou cota esgotada": aumento de `permanente|cota` > 0 em 15 min.
  - "Falhas de envio acima de 20%": transitórias / total > 0,2 com ≥ 5 envios em 15 min, por 5 min.
- RF07 — Regra do gitleaks para a chave do Resend (`re_…_…`). `.env.example` documenta as variáveis e o limite de domínio.

## Requisitos não funcionais

- RNF01 — HTTP cru (stdlib), sem SDK: nenhuma dependência nova.
- RNF02 — Nenhum e-mail, token ou chave em logs ou erros.
- RNF03 — Testes sem rede: servidor HTTP falso local.

## Critérios de aceite

- [x] CA01 — Requisição: método, caminho, `Authorization`, `Idempotency-Key`, `Content-Type`, remetente, destinatário, assunto, HTML, texto e 2 anexos com `content_id`, `image/png` e PNG válido em base64.
- [x] CA02 — Classificação: cota diária e mensal → cota; taxa, 503 e concorrente → transitório; 422, 403 e idempotência com outro corpo → permanente; timeout → transitório; nenhum erro contém o e-mail ecoado pelo provedor; sem chave → recusado.
- [x] CA03 — Consumidor: cada classe → resultado da métrica e ack/DLQ/redelivery corretos.
- [x] CA04 — Config: Resend completo aceito; sem chave recusado; provedor desconhecido recusado; fake em produção recusado.
- [x] CA05 — `/metrics` com `provedor` e `tipo`; stack com 16 alertas.
- [x] CA06 — CI verde; gitleaks detecta a chave `re_`.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Adapter contra servidor falso | unit | CA01, CA02 |
| Resultado por classe | unit | CA03 |
| Config | unit | CA04 |
| Labels e provisionamento | unit (`telemetria`) + integração (stack) | CA05 |

## Arquivos que serão criados

- `internal/notificacao/resend.go`, `internal/notificacao/resend_test.go`
- `docs/tasks/0029-provedor-resend.md`, `docs/prd/0029-provedor-resend.md`

## Arquivos que serão modificados

- `internal/notificacao/consumidor.go`, `internal/config/{config.go, config_test.go}`, `internal/telemetria/{telemetria.go, telemetria_test.go}`, `cmd/morfeu/main.go`
- `configs/grafana/provisioning/alerting/alertas.yml`, `test/observabilidade/stack_integration_test.go`, `.env.example`, `.gitleaks.toml`
- `docs/tasks/README.md`, `plan.md`, `state.md`

Total: 17.

## Dependências utilizadas

Nenhuma nova.

## Riscos

- Cota de 100/dia do plano grátis → DLQ + alerta + replay no dia seguinte. O teste de carga usa o fake.

## Estratégia de rollback

Reverter o merge. Com `EMAIL_PROVEDOR=fake`, o comportamento é o da 0028.

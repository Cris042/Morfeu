# Refinamento — Épico E10 (Observabilidade completa) — 2026-10-01

Cerimônia por épico (roles.md §6.14): brief único → pareceres (`arquiteto`, `security`, `qa`, `sre-devops`, depois `backend-dev`) → debate → 2 perguntas escaladas e **respondidas pelo usuário no mesmo dia**. **ADR 0012** (tail sampling no Alloy) autorizado.

## Pareceres

| Agente | Posição | Pontos-chave |
|---|---|---|
| arquiteto | seguir com ressalvas | Tempo monolítico (local, 72 h, ~384 MB); **tail sampling no Alloy** (app exporta 100%); `otlptracehttp` no app; exporter só em `telemetria`; exemplars best effort; Postgres como datasource descartado; ocupação sem label por sessão; propôs ADR |
| security | seguir com condições | sanitização **no app** antes de exportar (rota templada, sem query/headers, otelpgx sem parâmetros, erros sem e-mail) + teste de ausência de token/e-mail; remoção no Alloy como defesa extra; Tempo/OTLP sem porta; dep nova com CVE; URL do heartbeat e webhook como segredo |
| qa | seguir | coletor falso (`httptest`) para o exporter; app não trava com coletor fora; `alloy validate`; smoke da stack (erro → trace no Tempo); lista **nominal** de regras; contrato dashboards × métricas existentes; aceite manual do watchdog adiado |
| sre-devops | seguir | Alloy 512 MB + `memory_limiter`, `decision_wait` 10 s; correlação Loki↔Tempo; 4 dashboards (funil, compensações/cancelamentos, ocupação/holds, e-mail/gateway); alertas de purga + texto do estorno; **dead man's switch** via healthchecks.io agora, monitor HTTP no deploy |
| backend-dev | seguir | viável em 2 tasks (~20 / ~16 arquivos); `otlptracehttp` v1.46.0 (sem gRPC no binário); `tail_sampling` confirmado no Alloy (Context7); batch com timeout curto; heartbeat desligado sem env |

## Debate (divergências e resolução)

1. **Head × tail sampling** → **Escalado**: usuário **autorizou o ADR 0012** (app 100%, Alloy decide: ERROR, > 300 ms, 10%).
2. **Limiar de "lento"** (300 ms × 1 s) → **Consenso**: 300 ms, alinhado ao SLO do E12 (sob carga, só a cauda acima do SLO é retida).
3. **OTLP gRPC × HTTP no app** → **Consenso**: HTTP (menos dependências — §1); gRPC só entre Alloy e Tempo (configuração).
4. **Watchdog** → **Escalado**: usuário escolheu **healthchecks.io agora** (regra sempre ativa → webhook de ping; URL por env, vazia = desligado); monitor HTTP da API fica para o E0c-CD.
5. **Separar o watchdog em task própria** (security) → **Consenso**: não — é provisioning pequeno na 0041; a parte dependente de URL pública já fica para o E0c-CD.
6. **Exemplars** → **Consenso**: best effort, não bloqueiam a 0040.
7. **Métrica nova de ocupação** → **Consenso**: não — painel com os agregados `reserva_*` existentes.

## Conclusão

- 2 tasks: **0040** (Tempo + OTLP + tail sampling + correlação) e **0041** (dashboards de negócio + alertas + watchdog). Dep nova: `otlptracehttp` v1.46.0 (Context7 + govulncheck + `lib.md`).
- Riscos: (1) PII/credencial em atributo de span; (2) memória do Alloy sob carga (E12); (3) traces incompletos da saga assíncrona.

## Exigências por task

### T1 (0040) — Tempo, OTLP e tail sampling
- `internal/telemetria`: exporter `otlptracehttp` em lote (fila limitada, timeout curto, sem bloquear); sampler `ParentBased(AlwaysOn)` com coletor configurado, taxa de cabeça como plano B; sem `OTEL_EXPORTER_OTLP_ENDPOINT` → descarte como hoje; app sobe com o coletor fora.
- Sanitização antes de exportar: `url.path`/`http.route` pela rota templada (o `/i/*` já é redigido — PRD 0034), sem query nem headers; otelpgx sem parâmetros; teste que exporta spans de `/i/<token>`, checkout com e-mail e erro, afirmando ausência de token/e-mail.
- Alloy: receptor OTLP HTTP interno → `memory_limiter` → `tail_sampling` (ERROR, > 300 ms, 10%; `decision_wait` 10 s; `num_traces` limitado) → Tempo; remoção de atributos de URL/headers; 512 MB.
- Tempo: imagem com tag fixa, monolítico, local, retenção 72 h, ~384 MB, volume nomeado, **sem porta publicada**.
- Grafana: datasource Tempo; Loki com derived field `trace_id` → Tempo; Tempo → logs.
- Testes: exporter com coletor falso; app sem coletor; sampler; sanitização; smoke da stack (erro → trace no Tempo); `doc.md` §13 atualizado; `lib.md`.

### T2 (0041) — Dashboards, alertas e watchdog
- 4 dashboards provisionados (funil do checkout; compensações/estornos/cancelamentos/presos; ocupação/holds; e-mail/gateway), UIDs fixos, só métricas existentes, sem labels de alta cardinalidade nem PII.
- Alertas: purga da auditoria parada > 48 h; **watchdog** (sempre ativo → contact point webhook de ping no healthchecks.io, URL por env, política própria com repetição de 5 min; sem URL = desligado); textos dos alertas de estorno cobrindo cancelamentos. Lista fechada → **18 regras**, conferida por **nome** no teste da stack.
- Teste de contrato dashboards × métricas expostas; runbook curto por alerta novo; aceite manual do watchdog e monitor HTTP registrados como pendência do E0c-CD.

### Perguntas escaladas ao usuário (respondidas em 2026-10-01)

1. Tail sampling no Alloy → **ADR 0012 autorizado**.
2. Watchdog → **healthchecks.io agora** (monitor HTTP no deploy).

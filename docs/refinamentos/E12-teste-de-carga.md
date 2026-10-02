# Refinamento — Épico E12 (Teste de carga k6) — 2026-10-01

Cerimônia por épico (roles.md §6.14): brief único → pareceres (`arquiteto`, `sre-devops`, `security`, `qa`, depois `backend-dev`) → debate → 2 perguntas escaladas e **respondidas pelo usuário no mesmo dia**. Sem ADR.

Fatos conferidos pela sessão principal antes da rodada do `backend-dev`: o histograma HTTP (otelecho) usa os buckets padrão do OTel — sem fronteira em 0,3 s, o p95 do SLO seria interpolado entre 0,25 e 0,5; a query de invariante de assento prevista no ADR 0006 para o teardown do E2E **não existe** (nasce aqui como fonte única); os limites por IP são constantes no `main` (`novoLimitador` em 3 pontos).

## Pareceres

| Agente | Posição | Pontos-chave |
|---|---|---|
| arquiteto | seguir com ressalvas | M5 em duas marcas (local indicativo × oficial na VM); flag `MORFEU_LOADTEST` com fail-fast (não um 3º `AMBIENTE`); só a escrita precisa do modo; invariante em um lugar; veredito no Prometheus; `--cpus` para o k6; sem dashboard de k6, gerador distribuído ou framework de cenários; sem ADR |
| sre-devops | seguir com ressalvas | open model (`constant-arrival-rate`) e `dropped_iterations` como invalidez; gerador ≤ ~70% de CPU; cenários baseline (quente/frio), ramp 50→400, SLO 300 req/s 10 min, misto 90/10, soak, disputa, checkout; seed com `ANALYZE`; buckets em torno de 300 ms; RSS do Alloy; `GOMEMLIMIT`; relatório com selo de validade e PromQL exata |
| security | seguir com ressalvas | modo de carga só com fakes e fora de produção (testes de boot); limites **por IP** sobem, nunca desligados; por dono/login/webhook reais; XFF não confiável só para o teste; gauge + alerta de modo de carga; `/__teste/pagar` 404 em produção; dataset `@example.test` e hash pré-computado; alvo nunca a URL de produção; k6 por digest; relatório sem hosts/segredos |
| qa | seguir com ressalvas | PromQL fixa e versionada (p95 e erro sem 409); thresholds do k6 = guarda-corpo; disputa = 1 vencedor + N−1 409, zero 5xx; smoke dos scripts (~20 s); seed determinístico; 3 repetições com mediana; critério objetivo de "provisório" × inválido |
| backend-dev | seguir com ressalvas | T1 ~22–26 arquivos: `limiteEfetivo(base, loadtest)` nos 3 pontos do `novoLimitador` (×1000 só por IP); View de buckets repetindo o `AttributeFilter` (evita stream duplicado); seed em SQL com `generate_series` e guarda do nome do banco; `deploy/carga/` com k6, seed, invariante e smoke; T2 ≈ 3–4 h de relógio local |

## Debate (divergências e resolução)

1. **`AMBIENTE=loadtest` × flag `MORFEU_LOADTEST`** → **Consenso**: flag — um 3º valor de ambiente cairia em ramos `!= producao` espalhados (§1 anti-overengineering); a flag só é aceita com gateway e e-mail fakes e `AMBIENTE != producao` (fail-fast), atendendo à exigência de segurança.
2. **Chave do limitador por header (`X-Load-Client`) × multiplicador × desligar** → **Consenso**: multiplicador só nos limites por IP — sem superfície nova no handler (§6.6) e o middleware segue exercitado; desligar foi descartado.
3. **Guarda do banco** (security pediu DSN/host do compose) → **Consenso**: com a flag ligada, o nome do banco precisa terminar em `_carga`, e o seed recusa outro nome (barato e verificável); checagem de host descartada (o compose de carga é local).
4. **`workflow_dispatch`** → **Consenso**: adiado até a VM (sem alvo público seria código morto).
5. **Repetições × tempo de relógio** → **Consenso**: o patamar do SLO (300 req/s, 10 min) roda 3× e o veredito usa a mediana; baseline, ramp, misto, disputa e checkout 1×; soak de 30 min local (o longo é da VM).
6. **Single-flight do cartaz** → **Consenso**: só se o cenário de cache frio mostrar stampede (entra na T2 com teste de concorrência e `lib.md`).
7. **Fechamento do M5** → **Escalado**: usuário decidiu **"indicativo fecha o E12"** — a validação oficial vira critério de aceite da E0c-CD.
8. **Gerador** → **Escalado**: **só este notebook** — k6 em container com CPU limitada; run inválido se o gerador passar de ~70% de CPU ou houver iterações descartadas; `/__teste/pagar` sem token (rede interna).

## Conclusão

- 2 tasks: **0045** (ferramental de carga) → **0046** (execução local indicativa + relatório + ajustes pequenos). Dep nova: imagem `grafana/k6` (versão + digest, `lib.md`); `golang.org/x/sync/singleflight` só se a T2 provar stampede.
- **M5 indicativo fecha o E12** (decisão do usuário); o **M5 oficial** (gerador fora da VM, mesmos scripts e queries) entra no checklist de aceite da E0c-CD, junto com a recalibração do Argon2 e o soak longo.
- Riscos: (1) modo de carga ligado em produção; (2) números locais contaminados lidos como oficiais; (3) p95 interpolado por bucket ruim; (4) seed esgotando assentos e inflando 409.

## Exigências por task

### T1 (0045) — Ferramental de carga
- `MORFEU_LOADTEST=1` → `Config.LoadTest`; `Validate` recusa com `AMBIENTE=producao`, gateway ≠ fake, e-mail ≠ fake ou banco cujo nome não termina em `_carga`; testes de boot table-driven.
- `limiteEfetivo(base, loadtest)` (×1000) aplicado **só** aos limites por IP (travas, pedidos, consulta, ingresso, login/registro por IP); por dono, por conta/e-mail e webhook inalterados; teste unitário.
- WARN no boot + gauge `morfeu_modo_loadtest` (0/1) + alerta `morfeu-modo-loadtest` (regra 20, por uid no teste da stack).
- `/__teste/pagar` registrada só com gateway fake **e** `AMBIENTE != producao`; teste de 404 em produção.
- View do histograma `http.server.request.duration` com fronteiras incluindo 0,3 s (confirmar o nome real do instrumento), repetindo o `AttributeFilter`; teste no registry (`le="0.3"`); conferir dashboards/alertas que usam `le`.
- `deploy/carga/`: `docker-compose.carga.yml` (app com a flag, `GOMEMLIMIT`, limites de CPU/memória próximos da VM, banco `morfeu_carga`; k6 com `--cpus` próprio), `seed.sql` determinístico (`generate_series`, `@example.test`, hash Argon2 literal pré-computado, sessões suficientes, `ANALYZE`, guarda do nome do banco, idempotente), `invariante.sql` (assento com mais de um hold ativo/convertido, mais de um ingresso ativo, pedido pago sem ingresso — exit ≠ 0 se houver linha), scripts k6 (`constant-arrival-rate`/`ramping-arrival-rate`, `expectedStatuses` com 409 na disputa, thresholds de guarda-corpo incluindo `dropped_iterations`), `smoke.sh` (~20 s: `k6 inspect` + checks + invariante).
- `docs/carga/queries.md` (PromQL de p95 por rota e de erro sem 409, janela do patamar descartando 60 s) e `docs/carga/runbook.md` (como rodar, critérios de validade, limpeza = recriar a stack, nunca contra a URL de produção); imagem `grafana/k6` por versão + digest no `lib.md`.

### T2 (0046) — Execução local e relatório (M5 indicativo)
- Executar baseline (quente e frio), ramp 50→400 req/s, SLO 300 req/s × 10 min (**3×, mediana**), misto 90/10, disputa (1 vencedor + N−1 409, zero 5xx), checkout fim a fim (SLI) e soak de 30 min; invariante verde depois de cada execução.
- Validade: CPU do gerador ≤ ~70%, `dropped_iterations = 0`; fora disso o run é inválido (não "provisório").
- Relatório `docs/carga/AAAA-MM-DD-local.md` com selo **"indicativo (local)"** no topo, ambiente (CPU, RAM, `.wslconfig`, versões, commit, digest do k6), dataset, PromQL e janelas em UTC, p50/p95/p99 server-side × client-side, erro, req/s alcançados, RSS do Alloy, pool do PG, alertas disparados, gargalos e decisões.
- Ajustes pequenos com teste de regressão (single-flight do cartaz só com stampede comprovado); mudança grande vira ADR/task nova; Argon2 só registrado (recalibrar na VM).
- Roadmap: M5 "indicativo ✅ / oficial no aceite da E0c-CD"; `docs/deploy-checklist.md` ganha o M5 oficial.

### Perguntas escaladas ao usuário (respondidas em 2026-10-01)

1. Fechamento do M5 → **indicativo fecha o E12**; oficial no aceite da E0c-CD.
2. Gerador → **só este notebook**.

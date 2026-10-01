# Refinamento — Épico E11 (Hardening + backup) — 2026-10-01

Cerimônia por épico (roles.md §6.14): brief único → pareceres (`arquiteto`, `sre-devops`, `security`, `qa`, depois `backend-dev`) → debate → 3 perguntas escaladas e **respondidas pelo usuário no mesmo dia**. **ADR 0013** (privilégios do PG e política de backup) autorizado.

Correção de fato feita na sessão principal antes da rodada do `backend-dev`: a trilha do operador é `eventos_auditoria` (migration 017, sem FK); `pedido_eventos` é o histórico do pedido (FK → `pedidos`, sem cascade). As migrations rodam no boot do app com a `DATABASE_URL` única.

## Pareceres

| Agente | Posição | Pontos-chave |
|---|---|---|
| arquiteto | seguir com ressalvas | `docker-compose.prod.yml` standalone (override só soma `ports:`); 3 roles (migrator, app, purge) com pool separado para a purga; `age` com chave privada fora da VM; rotação por lifecycle do bucket; alerta de disco já existe; limpeza operacional fora do núcleo; propôs ADR |
| sre-devops | seguir com ressalvas | preflight do `.env` de prod (sem placeholder, tamanho mínimo); logging `json-file` com `max-size`; container de backup (alpine + pg16 client + age + rclone) com agendador versionado; stream com `pipefail`, tamanho mínimo; RTO medido no restore; restore mensal manual |
| security | seguir com ressalvas | zero `ports:` públicos (dev publica 5432/6379/5672 em 0.0.0.0 — alto se copiado); senha do Redis fora de `command:`; `${VAR:?}`; app não-owner, sem DELETE/TRUNCATE/TRIGGER na trilha; credencial do bucket **sem delete** + lifecycle; dump com role de leitura; LGPD: retenção do backup documentada e runbook reaplica pseudonimizações |
| qa | seguir com ressalvas | teste Go estático do YAML de prod (forma curta e longa, `network_mode: host`, caso negativo); testcontainers provando `permission denied`; ida e volta dump→cifra→restore→invariantes (invariantes listadas no PRD); matriz de erro (dump corrompido, chave errada, bucket fora, banco vazio); limpeza com matriz de borda |
| backend-dev | seguir com ressalvas | T1 ~18–22 arquivos: `redis.ParseURL` (hoje `Addr` cru — sem senha), `DATABASE_MIGRATE_URL`/`DATABASE_PURGE_URL` opcionais com fallback, migration de grants idempotente; T2 ~14–18 arquivos, shell em container (não subcomando Go), heartbeat; ida e volta com MinIO 60–120 s → smoke no PR e ciclo completo fora; limpeza cabe numa 0044 de ~10 arquivos |

## Debate (divergências e resolução)

1. **Redis: ACL (`-@dangerous`) × `requirepass`** → **Consenso**: `requirepass` lido de arquivo/env (fora de `command:`), healthcheck com `REDISCLI_AUTH`; ACL fica como melhoria — o exporter precisa de `INFO`/`CONFIG` e há um só cliente lógico (§1 anti-overengineering).
2. **Alerta de backup parado: métrica textfile × heartbeat externo** → **Consenso**: heartbeat no healthchecks.io após upload verificado (mesmo padrão do watchdog, funciona com a VM fora); sem métrica nova (§1).
3. **Rotação: lifecycle do bucket × código testável com relógio injetável** → **Consenso**: lifecycle (`diario/` 7 dias, `semanal/` ~31 dias — o dump de domingo vai para `semanal/`) + credencial sem delete (§6.6 segurança vence); rotação em código só como plano B se a Oracle não oferecer, com a trava de mínimo de cópias.
4. **Roles também no dev local** (arquiteto sim × backend-dev não) → **Consenso**: não — dev/CI seguem com usuário único pelo fallback das URLs; o drift é coberto por um teste de integração que cria os roles e roda migrate → app → purga com eles (§6.7).
5. **Ida e volta com MinIO no PR × agendado** → **Consenso**: no PR o ciclo dump → cifra → decifra → `pg_restore` → invariantes com PG em testcontainers; o ciclo com MinIO em target/build tag separado (manual ou agendado); o restore real mensal é manual (a chave privada fica fora da VM).
6. **ADR** → **Escalado**: usuário **autorizou o ADR 0013**.
7. **Guarda da chave privada do `age`** → **Escalado**: **gerenciador de senhas + cópia offline**.
8. **Limpeza operacional** → **Escalado**: **task 0044 no E11, sem pedidos** (processed_messages, holds terminais, outbox publicada); pedidos abandonados ficam (volume mínimo; têm histórico com FK).

## Conclusão

- 3 tasks: **0042** (hardening do compose + senha do Redis + roles do PG) → **0043** (backup e restore) → **0044** (limpeza operacional, não bloqueia o deploy). Deps novas (0043): `age`, `rclone` (Context7 + CVE + `lib.md`).
- **O §14.5 só fecha na E0c-CD**: NSG, SSH só chave, forced command, bucket real e primeiro restore real são aceite do deploy (checklist com comando de verificação por item).
- Riscos: (1) perda da chave privada; (2) grant esquecido numa tabela nova (app sem acesso em prod); (3) S3/lifecycle da Oracle diferente do MinIO; (4) backup parando em silêncio.

## Exigências por task

### T1 (0042) — Hardening do compose, senha do Redis e roles do PG
- `docker-compose.prod.yml` **standalone** (infra + observabilidade; app e Caddy entram na E0c-CD): nenhum `ports:` público, `${VAR:?}` para senhas de PG/Redis/RabbitMQ/Grafana, `logging` `json-file` com `max-size`/`max-file`, `restart`, `mem_limit`.
- Preflight (`make preflight` / script) do `.env` de prod: rejeita vazio, placeholders (`troque-esta-senha`, `postgres`, `morfeu`…) e senhas curtas (< 24 caracteres).
- Teste Go estático do YAML de prod: falha com `ports:` fora de `127.0.0.1` (forma curta e longa), com `network_mode: host` e com qualquer porta em Prometheus/Loki/Tempo/Alloy/exporters; allowlist vazia documentada (Caddy na E0c-CD); **caso negativo** com compose sintético `5432:5432`.
- Redis com `requirepass` (fora de `command:` visível); `REDIS_URL` aceita `redis://:senha@host:6379/0` via `redis.ParseURL` (compat `host:porta`); `redis_exporter` e healthcheck com a senha; app falha no boot em `AMBIENTE=producao` com Redis sem credencial.
- Roles (ADR 0013): init script em `configs/postgres` cria `morfeu_migrator`/`morfeu_app`/`morfeu_purge`/`morfeu_backup` com senhas por env; migration 018 de `GRANT`/`REVOKE` idempotente (`DO $$ … IF EXISTS`) + `ALTER DEFAULT PRIVILEGES`; `DATABASE_MIGRATE_URL` e `DATABASE_PURGE_URL` opcionais com fallback; pool próprio da purga no worker.
- Teste de integração: com os roles, migrate → app (CRUD ok; `DELETE`/`TRUNCATE` em `eventos_auditoria` → `permission denied`; app não é owner) → purga (`DELETE` ok; `UPDATE` e outras tabelas negados) → backup (só leitura).
- `docs/deploy-checklist.md`: NSG 22/80/443, SSH só chave, forced command, UFW, `.env` 600 — **um comando de verificação por item**, marcado como aceite da E0c-CD (não "feito").
- `docs/observabilidade.md`/runbook: confirmar que `morfeu-disco-80` cobre a raiz e o volume de dados.

### T2 (0043) — Backup e restore
- Imagem de backup (alpine + `postgresql16-client` + `age` + `rclone`, versões fixas, ARM64) com agendamento noturno versionado; `pg_dump -Fc | age -r <pub> | rclone rcat` com `set -o pipefail`; dump nunca em claro no disco; `sha256` do objeto; tamanho mínimo; `pg_dumpall --globals-only --no-role-passwords`; dump com `morfeu_backup`.
- Domingo → `semanal/`, demais → `diario/`; lifecycle documentado (7 dias / ~31 dias); credencial do job sem delete (política IAM documentada); plano B registrado.
- Heartbeat do backup (URL por env, vazia = desligado) só após upload verificado; alerta de backup parado fora da VM; nada de URL/credencial em log.
- `restore.sh`: baixa, confere `sha256`, decifra com a chave fornecida pelo operador, `pg_restore` em PG efêmero, invariantes **listadas no PRD** (versão da migration, contagem por tabela, FKs, triggers da trilha, índice único da trava), tempo medido (RTO), destrói o container ao final.
- Testes: ida e volta no PR (PG testcontainers, chave age de teste, dado sintético); matriz de erro (dump corrompido, chave errada, upload falhando → sem heartbeat, banco vazio rejeitado); ciclo com MinIO fora do PR.
- Runbook de restore e de perda de chave; nota LGPD (retenção ~31 dias, reaplicar pseudonimizações pós-dump); `lib.md` com `age` e `rclone`.

### T3 (0044) — Limpeza operacional
- Jobs em lotes no worker (padrão da purga da 0037): `processed_messages` após a janela do dedup do replay (valor fixo no PRD, maior que o prazo do replay da DLQ), holds terminais (nunca ativo nem ligado a pedido em andamento), outbox publicada.
- **Pedidos não são apagados.** A limpeza usa o role do app, nunca o da purga da trilha.
- Testes de borda da janela (antes/exata/depois), lote, idempotência e concorrência com o sweeper; métrica de linhas apagadas + alerta de job parado (por uid).

### Perguntas escaladas ao usuário (respondidas em 2026-10-01)

1. ADR de privilégios e backup → **ADR 0013 autorizado**.
2. Chave privada do `age` → **gerenciador de senhas + cópia offline**.
3. Limpeza operacional → **0044 no E11, sem pedidos**.

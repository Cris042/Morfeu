# PRD 0042 — Hardening do compose, senha do Redis e roles do PG (E11, T1)

- **Task:** docs/tasks/0042-hardening-roles.md
- **Branch:** feature/0042-hardening-roles
- **Data:** 2026-10-01
- **Status:** concluído

## Objetivo

Fechar, no que dá para fechar sem a VM, o §14.5 do `doc.md`: um compose de produção sem portas públicas e com senhas obrigatórias, Redis com senha e o banco com menor privilégio (ADR 0013). Fontes: `docs/refinamentos/E11-hardening-backup.md` §T1 e ADR 0013.

## Escopo

`docker-compose.prod.yml` (infra + observabilidade), preflight do `.env` de produção, senha do Redis no app/exporter/healthcheck, roles do PG (init + migration 018 + URLs de migrate e purga), testes estáticos e de integração, checklist da E0c-CD.

## Fora de escopo

- App e Caddy no compose de produção, NSG, SSH, forced command, UFW (E0c-CD — entram como checklist verificável).
- ACL do Redis (melhoria futura — refinamento E11 §debate 1).
- Backup (0043) e limpeza operacional (0044). O role `morfeu_backup` nasce aqui; o job de backup é da 0043.
- Containers non-root/read-only (Fase 2, `doc.md` §14.8).

## Requisitos funcionais

- RF01 — `docker-compose.prod.yml` **standalone** com postgres, redis, rabbitmq e a stack de observabilidade: **nenhum `ports:`** exceto Grafana e management do RabbitMQ em `127.0.0.1`; senhas por `${VAR:?}`; `logging` `json-file` com `max-size: 10m` e `max-file: 3` em todos os serviços; `restart: unless-stopped`; `mem_limit` em todos.
- RF02 — `scripts/preflight.sh <arquivo.env>…` (alvo `make preflight`): falha quando uma variável obrigatória está vazia, contém placeholder conhecido (`troque`, `postgres`, `morfeu`, `guest`, `changeme`, `senha`) ou tem senha com menos de 24 caracteres; não imprime valores.
- RF03 — Redis com `requirepass` lido de arquivo gerado no start (senha fora do `command:`/`ps`); healthcheck com `REDISCLI_AUTH`; `redis_exporter` com `REDIS_PASSWORD`.
- RF04 — `REDIS_URL` aceita `redis://[:senha@]host:porta/db` (via `redis.ParseURL`) e o formato `host:porta` atual; com `AMBIENTE=producao`, URL sem senha recusa o boot.
- RF05 — Roles (ADR 0013): `configs/postgres/02-roles.sh` cria `morfeu_migrator` (dono do banco, DDL), `morfeu_app`, `morfeu_purge` e `morfeu_backup` (`pg_read_all_data`) com senhas por env (role sem senha = não criado), e os `ALTER DEFAULT PRIVILEGES` do migrator para o app (DML em tabelas, uso de sequências).
- RF06 — Migration 018 idempotente: se os roles existem, `REVOKE DELETE, TRUNCATE, TRIGGER ON eventos_auditoria FROM morfeu_app` e `GRANT SELECT, DELETE ON eventos_auditoria TO morfeu_purge`; sem os roles (dev/CI) é no-op.
- RF07 — `DATABASE_MIGRATE_URL` (migrations no boot) e `DATABASE_PURGE_URL` (pool próprio, máx. 2 conexões, só a purga da trilha no worker) opcionais; ausentes = `DATABASE_URL` (dev, CI e testes inalterados).
- RF08 — Compose de dev publica PG/Redis/RabbitMQ só em `127.0.0.1` (o arquivo de dev copiado para uma VM não expõe nada).
- RF09 — `docs/deploy-checklist.md`: NSG 22/80/443, SSH só chave, forced command, UFW, `.env` com permissão 600, `nmap` externo — um comando de verificação por item, marcado como aceite da E0c-CD.

## Requisitos não funcionais

- RNF01 — Nenhuma senha em arquivo versionado nem em migration; exemplos só com placeholders que o preflight rejeita.
- RNF02 — Sem dependência nova (YAML pelo `gopkg.in/yaml.v3` já presente).

## Critérios de aceite

- [x] CA01 — Teste estático do `docker-compose.prod.yml`: nenhum `ports:` fora de `127.0.0.1` (formas curta e longa), nenhum `network_mode: host`, nenhuma porta em Prometheus/Loki/Tempo/Alloy/exporters/PG/Redis/RabbitMQ AMQP, senhas por `${VAR:?}`, logging com rotação em todos; **caso negativo** (compose sintético com `5432:5432` e com `host_ip: 0.0.0.0`) falha a validação.
- [x] CA02 — Preflight: `.env` válido passa; vazio, placeholder e senha curta falham, sem ecoar o valor.
- [x] CA03 — Integração (PG real com o `02-roles.sh` de verdade): migrate como `morfeu_migrator` → `morfeu_app` faz CRUD, não é dono de nenhuma tabela e recebe `permission denied` (42501) em `DELETE`/`TRUNCATE` da trilha → `morfeu_purge` roda `auditoria.Purgar`, mas não faz `UPDATE` na trilha nem lê `pedidos` → `morfeu_backup` lê tudo e não escreve.
- [x] CA04 — Config: `REDIS_URL` nos dois formatos; produção sem senha no Redis falha; fallback das URLs de migrate e purga.
- [x] CA05 — Integração do Redis com senha: sem senha → `NOAUTH`; com a URL → `PING` ok.
- [x] CA06 — `docs/deploy-checklist.md` e runbook (`docs/observabilidade.md`: alerta de disco cobre a raiz; senha do Redis no exporter).
- [x] CA07 — CI verde (`-race`, lint, govulncheck).

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| YAML de produção + casos negativos | unitário (`test/infra`, sem containers) | CA01 |
| Preflight com `.env` sintéticos | unitário (`test/infra`, roda o script) | CA02 |
| Roles, migrate, purga, backup | integração (`test/infra`, testcontainers) | CA03 |
| `REDIS_URL` e validação de produção | unitário (`internal/config`) | CA04 |
| Redis com senha | integração (`test/infra`) | CA05 |

## Plano de implementação

1. Config + main (URLs, Redis); 2. init dos roles + migration 018; 3. compose de prod + dev em 127.0.0.1 + exemplos de env; 4. preflight + Makefile; 5. testes; 6. docs.

**Skills de apoio (§4.4):** `golang-database`, `docker-patterns`.

## Arquivos que serão criados

- `docker-compose.prod.yml`, `.env.prod.example`, `configs/postgres/02-roles.sh`, `migrations/018_privilegios.{up,down}.sql`, `scripts/preflight.sh`
- `test/infra/compose_prod_test.go`, `test/infra/preflight_test.go`, `test/infra/privilegios_integration_test.go`
- `docs/deploy-checklist.md`, `docs/tasks/0042-hardening-roles.md`, `docs/prd/0042-hardening-roles.md`

## Arquivos que serão modificados

- `internal/config/config.go`, `internal/config/config_test.go`, `cmd/morfeu/main.go`
- `docker-compose.yml`, `.env.docker-compose.example`, `.env.observability.example`, `Makefile`
- `docs/observabilidade.md`, `docs/tasks/README.md`, `docs/roadmap.md`, `plan.md`, `state.md`

Total: 26 (desvios da implementação: `.gitignore` — `!.env.prod.example`, já que `.env.*` é ignorado; `cmd/morfeu/redis_test.go` — testes do `novoRedis`).

## Dependências utilizadas

Nenhuma nova (`go-redis` v9 `ParseURL`, `golang-migrate`, `testcontainers-go`, `yaml.v3` já no `go.mod`).

## Impactos técnicos

- Volume de PG já existente não roda o `02-roles.sh` (só volume novo) — runbook com o comando manual.
- Tabela nova criada por outro usuário que não o migrator não recebe os default privileges — migrations sempre pelo `DATABASE_MIGRATE_URL` em produção.

## Riscos

- Grant esquecido (app sem acesso em produção) → default privileges + teste que roda CRUD com o role do app.
- Teste estático passando por parse errado → casos negativos obrigatórios.

## Desvios registrados na implementação

- Redis de prod: `setpriv --reuid redis` (a `redis:7-alpine` não tem `su-exec`); config com a senha em `/tmp/redis.conf` modo 600.
- `docker compose up` de prod usa dois `--env-file` (`.env.prod` e `.env.observability`); `REDIS_PASSWORD` comentado no `.env.observability.example` (linha vazia sobrescreveria a do `.env.prod`).
- Preflight escolhe o perfil pelo nome do arquivo; URLs do Discord e do heartbeat exigem `https://`.
- Down da 018 devolve ao app só `DELETE` (TRUNCATE/TRIGGER nunca foram dele — os default privileges só dão DML).
- O alerta de disco (`mountpoint="/"`) cobre os volumes se o `data-root` do Docker ficar na raiz — item do checklist da E0c-CD.

## Estratégia de rollback

Reverter o merge; migration 018 tem `down` (devolve DELETE/TRUNCATE/TRIGGER ao app e revoga do purge); URLs novas são opcionais.

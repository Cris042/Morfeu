# ADR 0013 — Privilégios do PostgreSQL e política de backup

- **Status:** aceito
- **Data:** 2026-10-01
- **Task/PRD relacionados:** refinamento E11 (`docs/refinamentos/E11-hardening-backup.md`), tasks 0042–0043; `doc.md` §7 (RPO/RTO) e §14.5

## Contexto

O `doc.md` exige backup diário cifrado com restore validado (RPO 24 h / RTO ~1 h) e hardening antes do primeiro deploy público. Hoje um único usuário de banco faz tudo: roda as migrations no boot do app, atende a API e o worker e pode apagar a trilha de auditoria do operador (`eventos_auditoria` — o trigger barra só UPDATE/TRUNCATE; achado da auditoria 0037). Também não há backup. As duas decisões são estruturais e difíceis de reverter depois que existirem dados reais: quem pode fazer o quê no banco e como o dump sai da VM sem virar um vazamento de PII. Os 5 agentes do refinamento E11 convergiram; o usuário autorizou este ADR.

## Escopo

Cobre: roles do PostgreSQL e suas permissões; formato, cifra, transporte, retenção e restore do backup. Não cobre: NSG/SSH/Caddy (checklist da E0c-CD), senha do Redis e compose de produção (0042, sem decisão estrutural), limpeza operacional (0044).

## Decisão

**Três roles com menor privilégio no banco, e backup `pg_dump | age | Object Storage` em stream, com a chave privada fora da VM e a rotação feita pelo bucket.**

**Roles**
- `morfeu_migrator` — dono do schema; só ele roda DDL (migrations no boot via `DATABASE_MIGRATE_URL`).
- `morfeu_app` — DML nas tabelas do app; **não é dono** de nada; sem `DELETE`, `TRUNCATE` e `TRIGGER` em `eventos_auditoria`.
- `morfeu_purge` — `SELECT` e `DELETE` só em `eventos_auditoria`; usado por um pool próprio no worker, só pela purga de 12 meses (`DATABASE_PURGE_URL`).
- `morfeu_backup` — `pg_read_all_data`, somente leitura, para o dump.
- Os roles nascem no bootstrap (script de init do PG, senhas por env com `ALTER ROLE`); **senha nunca em migration**. A migration de `GRANT`/`REVOKE` é idempotente e não falha quando os roles não existem — dev e CI seguem com o usuário único (as URLs novas caem em `DATABASE_URL` quando ausentes). Um teste de integração cria os roles e prova os `permission denied`.

**Backup**
- `pg_dump -Fc` → `age` (destinatário X25519; **só a chave pública na VM**) → upload S3-compatível ao Oracle Object Storage, em stream (`set -o pipefail`; o dump em claro nunca toca o disco), mais `sha256` do objeto cifrado e um tamanho mínimo para contar como sucesso.
- Globais por `pg_dumpall --globals-only --no-role-passwords` (as senhas voltam pelo bootstrap).
- Container de backup próprio (alpine + `postgresql16-client` + `age` + `rclone`, versões fixas) com agendamento versionado no repositório, 1 execução por noite.
- **Retenção pelo bucket:** prefixos `diario/` (7 dias) e `semanal/` (o dump de domingo, ~31 dias) com regras de lifecycle do Object Storage; a credencial do job **não tem permissão de delete** — uma VM comprometida não apaga nem reescreve backups.
- Sucesso só após upload verificado → ping de heartbeat (healthchecks.io, mesmo padrão do watchdog) → alerta de backup parado fora da VM.
- **Chave privada:** no gerenciador de senhas do dono **e** numa cópia offline (decisão do usuário). Por isso o restore real (mensal) é manual, em máquina do dono, num PG efêmero destruído ao final, com verificação de invariantes e RTO medido.

## Tecnologias ou padrões envolvidos

PostgreSQL roles/GRANT, `pg_read_all_data`, `pg_dump`/`pg_restore` custom format, `age` (X25519), `rclone` (S3), Oracle Object Storage lifecycle, healthchecks.io, testcontainers (PG + MinIO).

## Benefícios

- Um bug ou injeção no app não apaga a trilha de auditoria nem altera o schema.
- VM comprometida não decifra nem apaga backups antigos.
- Backup e retenção sem código de rotação próprio (menos lógica para errar e testar).

## Trade-offs

- Mais credenciais para operar (4 roles + chave age + credencial do bucket) e um segundo pool no worker.
- Restore depende de uma pessoa ter a chave privada — sem ela o backup é inútil.
- Dado apagado/pseudonimizado sobrevive nos backups por até ~31 dias (LGPD).
- Dev e CI rodam com usuário único: o modelo de permissão real só é exercitado pelo teste de integração e em produção.

## Riscos

- **Perda da chave privada** — prob. baixa, impacto crítico. Mitigação: duas cópias (gerenciador + offline) e runbook.
- **Grant esquecido numa tabela nova** (app sem acesso em produção) — prob. média, impacto alto. Mitigação: `ALTER DEFAULT PRIVILEGES` do migrator para o `morfeu_app` + teste de integração que roda o app com os roles.
- **Lifecycle/credencial sem delete indisponíveis ou diferentes na Oracle** — prob. média, impacto médio. Mitigação: verificação no aceite da E0c-CD; plano B é rotação no script com credencial separada e trava de mínimo de cópias.
- **Backup parando em silêncio** — prob. média, impacto alto. Mitigação: heartbeat externo + alerta.

## Estratégias para minimizar os trade-offs

- URLs novas opcionais com fallback para `DATABASE_URL`: dev, CI e testes existentes não mudam.
- Runbook de restore reaplica pseudonimizações/exclusões feitas depois do dump; retenção do backup documentada na política de privacidade.
- Teste de ida e volta (dump → cifra → restore → invariantes) no CI; ciclo com MinIO fora do PR.

## Condições de reversão

Revisitar se: (a) surgir um segundo operador com acesso ao banco (aí roles por pessoa); (b) o volume passar a exigir PITR (WAL archiving) em vez de dump diário; (c) a Oracle não oferecer lifecycle com credencial sem delete.

## Impacto esperado

`internal/config` e `cmd/morfeu` (URLs de migrate e purga, segundo pool no worker), migration de grants, `configs/postgres` (init dos roles), `docker-compose.prod.yml`, imagem/scripts de backup e restore, alerta de backup parado, `lib.md` (`age`, `rclone`), runbook em `docs/`.

## Alternativas consideradas e descartadas

- **Usuário único com mais triggers (também contra DELETE):** a purga legítima precisaria desligar o trigger — o mesmo usuário poderia fazê-lo.
- **Cifra simétrica (`openssl enc`/`gpg -c`) com senha na VM:** a VM comprometida decifra todo o histórico; `openssl enc` ainda sem autenticação.
- **`gpg` assimétrico:** mesmo modelo do `age`, com keyring e UX piores.
- **Rotação por código com credencial de delete:** mais lógica para testar e uma VM comprometida apagaria os backups.
- **Subcomando Go `-mode=backup`:** obrigaria `pg_dump`/`age` dentro da imagem `scratch` do app.
- **PITR com WAL archiving (pgBackRest/WAL-G):** excede o RPO de 24 h definido e o orçamento de tempo do dev solo.

## ADRs relacionados

- ADR 0002 — complementa (camada de dados: o pool do app passa a usar um role sem DDL).
- ADR 0006 — implementa o "restore mensal em container efêmero + invariantes".
- ADR 0007 — sem impacto (RabbitMQ fora do backup: mensagens são reconstituíveis pela outbox).

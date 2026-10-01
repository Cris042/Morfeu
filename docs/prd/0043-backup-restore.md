# PRD 0043 — Backup cifrado e restore validado (E11, T2)

- **Task:** docs/tasks/0043-backup-restore.md
- **Branch:** feature/0043-backup-restore
- **Data:** 2026-10-01
- **Status:** implementado — aguardando gate (CA08 = CI)

## Objetivo

RPO 24 h / RTO ~1 h (`doc.md` §7) com o desenho do ADR 0013: `pg_dump | age | Object Storage` em stream, chave privada fora da VM, retenção pelo bucket com credencial sem delete, heartbeat externo e restore validado por invariantes. Fontes: `docs/refinamentos/E11-hardening-backup.md` §T2 e ADR 0013.

## Escopo

Imagem `deploy/backup/Dockerfile` + scripts em `scripts/backup/`, serviço `backup` no `docker-compose.prod.yml`, preflight das variáveis, testes, runbook `docs/backup.md`, `lib.md`.

## Fora de escopo

- Bucket, lifecycle, política IAM e credencial reais da Oracle; primeiro backup e primeiro restore reais (aceite da E0c-CD — checklist).
- PITR/WAL; backup de Redis (cache) e RabbitMQ (mensagens reconstituíveis pela outbox).
- Limpeza operacional (0044).

## Requisitos funcionais

- RF01 — Imagem de backup: `alpine:3.22.6` com `age` 1.2.1-r10, `rclone` 1.75.1 (release oficial com SHA256 fixado), `postgresql16-client` 16.15-r0, `bash`, `curl` (versões fixas); roda como usuário não-root; build ARM64/AMD64.
- RF02 — `scripts/backup/backup.sh` (uma execução): confere que o banco tem `schema_migrations` limpo (sem versão = banco vazio/errado → falha); nome `morfeu-<UTC AAAAMMDDTHHMMSSZ>`; domingo (UTC) → prefixo `semanal/`, demais → `diario/`; `set -euo pipefail`; `pg_dump -Fc` como `morfeu_backup` → `age -r $BACKUP_AGE_RECIPIENT` → `rclone rcat` — **o dump em claro nunca toca o disco**; `sha256` e tamanho do cifrado calculados no stream; sobe `<nome>.dump.age`, `<nome>.sha256` e `<nome>.globals.age` (`pg_dumpall --globals-only --no-role-passwords`, cifrado) e um `<nome>.manifesto` (versão da migration e contagem de linhas por tabela — só números); verifica o objeto no destino (tamanho = calculado) e o tamanho mínimo (`BACKUP_TAMANHO_MINIMO`, padrão 1024 bytes).
- RF03 — Sucesso → ping em `BACKUP_HEARTBEAT_URL`; falha → ping em `<url>/fail` (healthchecks.io); URL vazia = sem ping; a URL e as credenciais nunca aparecem em log; saída ≠ 0 em qualquer falha.
- RF04 — `scripts/backup/agendar.sh`: laço que dorme até `BACKUP_HORA_UTC` (padrão `06`, 03:00 em Brasília) e roda o `backup.sh`; falha não derruba o laço; log de uma linha por execução (início, fim, resultado, tamanho, duração).
- RF05 — `scripts/backup/restore.sh <objeto>`: baixa o objeto e o `.sha256`, confere o hash, decifra com a identidade em `BACKUP_AGE_IDENTITY` (arquivo fornecido pelo operador, nunca da VM), `pg_restore --no-owner --no-acl` num PG efêmero (`RESTORE_PG*`), roda `scripts/backup/invariantes.sql` e compara com o manifesto; imprime o tempo total (RTO).
- RF06 — Invariantes: versão da migration igual à do manifesto e não suja; mesma lista de tabelas; contagem por tabela igual ao manifesto; nenhuma constraint `NOT VALID`; triggers da trilha (`eventos_auditoria_sem_update`/`_sem_truncate`) presentes; índice único parcial da trava (ADR 0008) presente.
- RF07 — `docker-compose.prod.yml` ganha o serviço `backup` (build local, sem `ports:`, `mem_limit`, logging com rotação, `restart`, depende do PG saudável); variáveis por `${VAR:?}` (destinatário age, credenciais S3, bucket, endpoint); `BACKUP_HEARTBEAT_URL` opcional.
- RF08 — Preflight valida `BACKUP_AGE_RECIPIENT` (começa com `age1`, sem chave privada `AGE-SECRET-KEY` em nenhum `.env`), credenciais S3 não vazias e heartbeat `https://` quando presente.
- RF09 — `docs/backup.md`: lifecycle do bucket (`diario/` 7 dias, `semanal/` 31 dias), política IAM sem `OBJECT_DELETE`/overwrite para o job, plano B (rotação no script com credencial separada e mínimo de cópias), geração e guarda da chave (gerenciador + cópia offline), restore mensal passo a passo com RTO, perda da chave, LGPD (backup guarda dado por até ~31 dias; restore reaplica pseudonimizações feitas depois do dump; container e arquivos temporários destruídos).

## Requisitos não funcionais

- RNF01 — Nenhuma chave privada, credencial ou URL de heartbeat versionada nem logada.
- RNF02 — Teste de ida e volta no CI de PR < 2 min (backend `local` do rclone); ciclo com MinIO opcional por env (`BACKUP_TESTE_S3=1`), fora do PR.

## Critérios de aceite

- [x] CA01 — Ida e volta (PG real com roles + migrations + dados sintéticos): backup → objeto cifrado (não contém um valor conhecido do seed) → restore em PG efêmero → invariantes ok.
- [x] CA02 — Chave errada falha o restore com mensagem clara.
- [x] CA03 — Objeto corrompido (byte alterado) falha na conferência do `sha256`.
- [x] CA04 — Destino indisponível → saída ≠ 0 e ping de falha, nunca de sucesso.
- [x] CA05 — Banco sem `schema_migrations` → backup recusado.
- [x] CA06 — `docker-compose.prod.yml` com o serviço `backup` passa o teste estático; preflight cobre as variáveis novas (inclui chave privada num `.env` → falha).
- [x] CA07 — `docs/backup.md`, `lib.md` e checklist da E0c-CD atualizados.
- [ ] CA08 — CI verde.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Ida e volta + invariantes | integração (`test/infra/backup_integration_test.go`, imagem real) | CA01 |
| Chave errada, objeto corrompido, destino fora, banco vazio | integração (mesmo arquivo) | CA02–CA05 |
| Compose e preflight | unitário (`test/infra`) | CA06 |
| S3 real (`rclone serve s3` da própria imagem; ver Desvios) | integração opcional (`BACKUP_TESTE_S3=1`) | RNF02 |

## Plano de implementação

1. Imagem + `backup.sh`; 2. `agendar.sh`; 3. `restore.sh` + `invariantes.sql`; 4. compose + preflight + exemplos de env; 5. testes; 6. runbook, checklist, `lib.md`.

**Skills de apoio (§4.4):** `docker-patterns`, `golang-testing`.

## Arquivos que serão criados

- `deploy/backup/Dockerfile`, `scripts/backup/{backup.sh, agendar.sh, restore.sh, invariantes.sql}`
- `test/infra/backup_integration_test.go`, `docs/backup.md`
- `docs/tasks/0043-backup-restore.md`, `docs/prd/0043-backup-restore.md`

## Arquivos que serão modificados

- `docker-compose.prod.yml`, `.env.prod.example`, `scripts/preflight.sh`, `test/infra/compose_prod_test.go`, `test/infra/preflight_test.go`, `test/infra/privilegios_integration_test.go` (helper `subirPGComRolesEm`, ver Desvios), `Makefile`
- `lib.md`, `docs/deploy-checklist.md`, `docs/tasks/README.md`, `docs/roadmap.md`, `plan.md`, `state.md`

Total estimado: 20.

## Dependências utilizadas

Novas, só na imagem de backup (não no `go.mod`): `age` 1.2.1 (cifra X25519 — escolhida no ADR 0013 sobre gpg/openssl), `rclone` 1.75.1 do release oficial (S3-compatível com stream por `rcat`; o pacote 1.69.3 do Alpine 3.22 tinha 24 advisories no OSV, 4 críticos — trocado no passe de revisão), `postgresql16-client` 16.15 (mesmo major do servidor). Context7: `/filosottile/age`, `/websites/rclone`. Registro em `lib.md`.

## Impactos técnicos

- Serviço novo no compose de prod (~64 MB); o `morfeu_backup` da 0042 passa a ser usado.
- `.env.prod` ganha as variáveis de backup (preflight cobre).

## Riscos

- S3 da Oracle diferente do MinIO/local (path-style, região) → aceite real na E0c-CD; endpoint e provider por env.
- Contagem do manifesto fora do snapshot do dump (escrita concorrente) → manifesto e dump na mesma transação de leitura (`pg_dump --snapshot` com snapshot exportado) ou divergência tolerada só se documentada no PRD.

## Estratégia de rollback

Reverter o merge (serviço isolado; o app não muda).

## Desvios (registrados na implementação)

1. **Snapshot exportado adotado** (sem desvio do risco): contagens do manifesto e `pg_dump --snapshot` usam o mesmo snapshot (`pg_export_snapshot()` numa sessão `psql` em `coproc` REPEATABLE READ aberta durante o dump). Funciona e é coberto pelo teste de ida e volta; ressalva de implementação: o coproc não é acessível dentro de `$(...)` (subshell fecha os FDs), então `consultar` grava em arquivo.
2. **Nome do índice da trava:** o rascunho do PRD citava `holds_assento_ativo`, mas a migration 010 o substituiu por `holds_assento_ocupado` (único parcial, `ativo`+`convertido`). As invariantes checam `holds_assento_ocupado` **e** `ingressos_assento_ativo` (2ª linha de defesa). O teste de ida e volta pegou o nome antigo.
3. **MinIO → `rclone serve s3`:** as imagens públicas do MinIO (`minio/minio` no Docker Hub, `quay.io/minio/minio`) não puxam mais. O ciclo S3 opcional (`BACKUP_TESTE_S3=1`) usa o servidor S3 do próprio rclone da imagem (protocolo S3, multipart, credenciais); passa. Credencial/endpoint da Oracle seguem para o aceite da E0c-CD.
4. **Contexto de build restrito:** o compose usa `context: ./scripts/backup` com `dockerfile: ../../deploy/backup/Dockerfile` (não há `.dockerignore` na raiz; contexto na raiz arrastaria `web/node_modules`). O teste monta o mesmo contexto num diretório temporário.
5. **`mem_limit` 128m** (PRD estimava ~64 MB): `rclone` S3 usa buffers de multipart (~20 MB) além do `pg_dump`/`age`.
6. **Arquivos fora da lista:** `test/infra/privilegios_integration_test.go` (o helper `subirPGComRoles` ganhou a variante `subirPGComRolesEm` com rede/alias — sem duplicar o bootstrap dos roles).
7. Extras: serviço com `init: true`, `read_only`, `cap_drop: ALL`, `no-new-privileges`; `make backup-teste`; ganchos de teste do agendador (`BACKUP_AGENDAR_SEM_ESPERA`, `BACKUP_AGENDAR_MAX_EXECUCOES`).

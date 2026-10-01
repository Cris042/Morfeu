# Backup cifrado e restore — runbook

> Task 0043, ADR 0013, `doc.md` §7 (RPO 24 h / RTO ~1 h). Mecanismo: `pg_dump -Fc | age | rclone rcat` em stream (o dump em claro nunca toca o disco), upload para o Object Storage S3-compatível da Oracle, retenção pelo **bucket**, heartbeat externo e restore validado por invariantes. Aceite real (bucket, IAM, primeiro backup/restore): `docs/deploy-checklist.md` ("Backup e restore").

## Como funciona

- Serviço `backup` do `docker-compose.prod.yml` (imagem `deploy/backup/Dockerfile`: `alpine:3.22.6` + `age` + `rclone` + `postgresql16-client`, usuário não-root, `read_only`, sem portas). O `scripts/backup/agendar.sh` dorme até `BACKUP_HORA_UTC` (padrão `06` = 03:00 em Brasília) e roda o `backup.sh`; uma linha de log por execução (`docker compose logs backup`).
- `backup.sh` conecta como **`morfeu_backup`** (`pg_read_all_data`, somente leitura). Recusa banco sem `schema_migrations` ou com migration suja. Conta as linhas de cada tabela e roda o `pg_dump` **no mesmo snapshot** (`pg_export_snapshot`), então o manifesto sempre bate com o dump mesmo com escrita concorrente.
- Objetos gravados em `diario/` (domingo UTC → `semanal/`), com o nome `morfeu-<AAAAMMDDTHHMMSSZ>`:
  | Arquivo | Conteúdo |
  |---|---|
  | `.dump.age` | `pg_dump -Fc` cifrado |
  | `.sha256` | hash do objeto **cifrado** |
  | `.globals.age` | `pg_dumpall --globals-only --no-role-passwords`, cifrado (as senhas voltam pelo bootstrap `02-roles.sh`) |
  | `.manifesto` | em claro, **só números**: versão da migration e contagem de linhas por tabela |
- Sucesso só depois de verificar o tamanho do objeto no destino e o tamanho mínimo (`BACKUP_TAMANHO_MINIMO`, padrão 1024 bytes). Sucesso → ping em `BACKUP_HEARTBEAT_URL`; qualquer falha → ping em `<url>/fail` e saída ≠ 0. A URL e as credenciais nunca vão para o log.

Variáveis (`.env.prod`; `make preflight` valida): `BACKUP_AGE_RECIPIENT` (só a chave **pública** `age1…`), `BACKUP_S3_ENDPOINT`, `BACKUP_S3_REGION`, `BACKUP_S3_BUCKET`, `BACKUP_S3_ACCESS_KEY_ID`, `BACKUP_S3_SECRET_ACCESS_KEY`, `BACKUP_HEARTBEAT_URL` (opcional, https), `PG_BACKUP_PASSWORD`.

## Bucket: lifecycle e permissões

1. Bucket **privado** exclusivo dos backups.
2. **Lifecycle** (Object Storage → bucket → Lifecycle Rules): excluir objetos com prefixo `diario/` após **7 dias** e `semanal/` após **31 dias**. A rotação é do bucket — o script nunca apaga nada.
3. **Credencial do job sem delete/overwrite** (Customer Secret Key de um usuário/grupo IAM só para o backup). Política mínima, restrita ao bucket (ajustar nomes):
   ```
   Allow group morfeu-backup to read objects in compartment <c> where target.bucket.name='<bucket>'
   Allow group morfeu-backup to manage objects in compartment <c> where all {target.bucket.name='<bucket>', any {request.permission='OBJECT_CREATE', request.permission='OBJECT_INSPECT'}}
   ```
   Isto dá criar/ler/listar e **não** dá `OBJECT_DELETE` nem `OBJECT_OVERWRITE`: uma VM comprometida não apaga nem reescreve backups. A listagem é necessária para a conferência de tamanho do `backup.sh`; o bucket já existe (`NO_CHECK_BUCKET=true`: a credencial não cria bucket).
4. **Plano B** (se a Oracle não permitir essa política ou o lifecycle): rotação no script com uma **credencial separada** só para o passo de limpeza, com trava de mínimo de cópias (nunca apagar se restarem menos de N objetos recentes). Exige reabrir o ADR 0013 (condição de reversão "c") — fale com o dono antes.
5. Para o restore, use a mesma credencial (ela lê) ou uma só de leitura.

## Chave age

- **Gerar fora da VM**, em máquina do dono: `age-keygen -o morfeu-backup.key` (imprime `Public key: age1…`). A **pública** vai em `BACKUP_AGE_RECIPIENT`; a **privada** (`AGE-SECRET-KEY-…`) nunca entra em `.env`, repositório ou VM (o preflight recusa).
- **Guardar em dois lugares:** gerenciador de senhas do dono **e** uma cópia offline (papel/pendrive em local seguro). Conferir os dois a cada restore mensal.
- **Perda da chave:** os backups existentes ficam **ilegíveis para sempre**. Passos: (1) gerar um novo par; (2) trocar `BACKUP_AGE_RECIPIENT` e subir o serviço; (3) rodar um backup manual e restaurá-lo para validar a chave nova; (4) registrar o incidente — o histórico cifrado com a chave antiga deve ser tratado como perdido (o banco vivo continua íntegro). **Chave comprometida:** mesmos passos, e apagar os backups antigos pelo console (com um usuário admin; o job não tem delete).

## Operação do dia a dia

```bash
# Backup manual (uma execução; além do agendamento da madrugada)
docker compose --env-file .env.prod --env-file .env.observability -f docker-compose.prod.yml exec backup bash /opt/backup/backup.sh
# Último resultado
docker compose --env-file .env.prod --env-file .env.observability -f docker-compose.prod.yml logs --tail 20 backup
```

Alerta de backup parado: o healthchecks.io avisa quando o ping diário não chega (a VM morta também é coberta, por estar fora dela).

## Restore mensal (passo a passo — máquina do dono, nunca a VM de produção)

Pré-requisitos: Docker; este repositório; a chave privada (dos dois lugares, ao menos um); credencial de leitura do bucket. O PG efêmero é **vazio** e **destruído ao final**.

```bash
# 1. imagem do restore (a mesma do backup) e rede/PG efêmeros
docker build -t morfeu-backup:local -f deploy/backup/Dockerfile scripts/backup
docker network create morfeu-restore
docker run -d --name pg-restore --network morfeu-restore \
  -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD="$(openssl rand -hex 16)" -e POSTGRES_DB=morfeu \
  postgres:16-alpine
# (guarde a senha gerada: RESTORE_PGPASSWORD abaixo)

# 2. escolha o objeto (mais recente de diario/ ou semanal/)
rclone lsf <remote>:<bucket>/diario/ --include '*.dump.age'   # ou use o console

# 3. restore + invariantes (a chave entra só como arquivo montado, somente leitura)
docker run --rm --network morfeu-restore --user "$(id -u):$(id -g)" \
  -v "$PWD/morfeu-backup.key":/chave/age.key:ro \
  -e BACKUP_AGE_IDENTITY=/chave/age.key \
  -e BACKUP_DESTINO=backup:<bucket> \
  -e RCLONE_CONFIG_BACKUP_TYPE=s3 -e RCLONE_CONFIG_BACKUP_PROVIDER=Other \
  -e RCLONE_CONFIG_BACKUP_ENDPOINT=<endpoint> -e RCLONE_CONFIG_BACKUP_REGION=<regiao> \
  -e RCLONE_CONFIG_BACKUP_ACCESS_KEY_ID=<id> -e RCLONE_CONFIG_BACKUP_SECRET_ACCESS_KEY=<segredo> \
  -e RCLONE_CONFIG_BACKUP_NO_CHECK_BUCKET=true \
  -e RESTORE_PGHOST=pg-restore -e RESTORE_PGUSER=postgres -e RESTORE_PGPASSWORD=<senha> -e RESTORE_PGDATABASE=morfeu \
  morfeu-backup:local bash /opt/backup/restore.sh diario/morfeu-<AAAAMMDDTHHMMSSZ>.dump.age
```

O `restore.sh` baixa o objeto e o `.sha256` (**sha256 não confere → aborta**: objeto corrompido ou adulterado), decifra (**chave errada → mensagem clara**), roda `pg_restore --no-owner --no-acl` e executa `invariantes.sql` contra o manifesto:

- versão da migration igual à do manifesto e não suja;
- mesma lista de tabelas e mesma contagem de linhas por tabela;
- nenhuma constraint `NOT VALID`;
- triggers `eventos_auditoria_sem_update` e `_sem_truncate` presentes;
- índices únicos parciais da trava de assento presentes (`holds_assento_ocupado`, `ingressos_assento_ativo` — ADR 0008).

Sucesso termina com `restore: ok — invariantes verificadas; tempo total (RTO) = Ns`. **Registre o RTO** (meta ~1 h) no `docs/deploy-checklist.md` no primeiro restore real e acompanhe a evolução com o volume.

```bash
# 4. (opcional) inspeção manual: docker exec -it pg-restore psql -U postgres morfeu
# 5. destruir tudo — o restore contém dados pessoais
docker rm -f pg-restore && docker network rm morfeu-restore
shred -u morfeu-backup.key 2>/dev/null || true   # só se foi uma cópia temporária da chave
```

Restore para **recuperação de desastre** (VM perdida): o mesmo procedimento, apontando `RESTORE_PG*` para o PG novo da VM (vazio, já com os roles do `02-roles.sh`); depois suba o app (que roda as migrations seguintes, se houver). O `.globals.age` guarda roles/tablespaces (sem senhas) para consulta; as senhas vêm do bootstrap.

Falhas comuns: `pg_restore` falha se o PG não estiver vazio; `permission denied` no restore indica usuário sem superusuário (a extensão `btree_gist` exige); `não foi possível decifrar` = chave errada ou arquivo que não é do backup.

## LGPD

- O backup guarda dado pessoal (e-mails, nomes) por até **~31 dias** (retenção do prefixo `semanal/`); a política de privacidade deve dizer isso.
- **Pseudonimização/exclusão pedida depois do dump não está no backup antigo.** Ao restaurar, **reaplique** as pseudonimizações/exclusões feitas desde a data do dump antes de colocar o banco restaurado em uso.
- O PG efêmero, o container de restore, a rede e qualquer arquivo temporário (a chave copiada, objetos baixados) são **destruídos** ao final do exercício. O `restore.sh` apaga o diretório temporário (objeto e dump decifrado) ao terminar, inclusive em falha.

## Testes

`make backup-teste` (ou o comando de `docs/ambiente-dev.md` com `-run TestBackup`): sobe PG com os roles e as migrations reais, semeia dados sintéticos, roda `backup.sh` com a imagem real (backend `local` do rclone), prova que o objeto é age e não contém o e-mail do seed, restaura num PG efêmero e roda as invariantes; cobre chave errada, objeto corrompido, destino fora (ping de falha, nunca o de sucesso), banco sem `schema_migrations` e o agendador. Com `BACKUP_TESTE_S3=1` repete o ciclo contra um servidor S3 (`rclone serve s3`) — fora do CI de PR.

#!/usr/bin/env bash
# Restore validado (task 0043, ADR 0013) — roda DENTRO da imagem de backup, em
# máquina do operador, contra um PG EFÊMERO. Uso: restore.sh <objeto>
#   <objeto> = caminho do .dump.age no destino (ex.: diario/morfeu-20261001T060000Z.dump.age)
#
# Env: BACKUP_DESTINO (+ RCLONE_CONFIG_*), BACKUP_AGE_IDENTITY (ARQUIVO da chave
#   privada, fornecido pelo operador — nunca fica na VM), RESTORE_PGHOST,
#   RESTORE_PGPORT, RESTORE_PGUSER, RESTORE_PGPASSWORD, RESTORE_PGDATABASE
#   (superusuário do PG efêmero; a extensão btree_gist exige).
# Imprime o tempo total (RTO). Código ≠ 0 em qualquer falha.
set -euo pipefail

INICIO=$SECONDS
DIR_SCRIPTS="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Remotes vêm só de RCLONE_CONFIG_* (env): sem arquivo de config, sem aviso.
export RCLONE_CONFIG="${RCLONE_CONFIG:-/dev/null}"
RCLONE_OPTS=(--contimeout 30s --timeout 5m --low-level-retries 5)

falhar() { echo "restore: ERRO: $*" >&2; exit 1; }
log() { echo "restore: $*"; }

[ "$#" -eq 1 ] || falhar "uso: restore.sh <diario|semanal>/<nome>.dump.age"
OBJETO="$1"
case "$OBJETO" in *.dump.age) ;; *) falhar "o objeto deve terminar em .dump.age" ;; esac
BASE="${OBJETO%.dump.age}"
[ -n "${BACKUP_DESTINO:-}" ] || falhar "BACKUP_DESTINO não definido"
[ -n "${BACKUP_AGE_IDENTITY:-}" ] && [ -r "$BACKUP_AGE_IDENTITY" ] || falhar "BACKUP_AGE_IDENTITY deve apontar para o arquivo da chave privada (legível)"
for v in RESTORE_PGHOST RESTORE_PGUSER RESTORE_PGPASSWORD RESTORE_PGDATABASE; do
  [ -n "${!v:-}" ] || falhar "$v não definido (PG efêmero de restore; nunca o banco de produção)"
done
DESTINO="${BACKUP_DESTINO%/}"

TMP="$(mktemp -d "${RESTORE_TMP:-${TMPDIR:-/tmp}}/restore.XXXXXX")"
chmod 700 "$TMP"
trap 'rm -rf "$TMP"' EXIT

log "baixando $OBJETO"
rclone copyto "${RCLONE_OPTS[@]}" "$DESTINO/$OBJETO" "$TMP/dump.age" || falhar "download do objeto falhou"
rclone cat "${RCLONE_OPTS[@]}" "$DESTINO/$BASE.sha256" >"$TMP/sha256" || falhar "download do .sha256 falhou"
rclone cat "${RCLONE_OPTS[@]}" "$DESTINO/$BASE.manifesto" >"$TMP/manifesto" || falhar "download do .manifesto falhou"

esperado="$(cut -d' ' -f1 "$TMP/sha256" | head -n1)"
real="$(sha256sum "$TMP/dump.age" | cut -d' ' -f1)"
[ -n "$esperado" ] && [ "$esperado" = "$real" ] || falhar "sha256 do objeto não confere — objeto corrompido ou adulterado"
log "sha256 conferido"

if ! age -d -i "$BACKUP_AGE_IDENTITY" -o "$TMP/dump" "$TMP/dump.age" 2>"$TMP/age.err"; then
  falhar "não foi possível decifrar: chave errada (não é a identidade deste backup) ou objeto inválido"
fi
rm -f "$TMP/dump.age"
log "decifrado"

export PGHOST="$RESTORE_PGHOST" PGPORT="${RESTORE_PGPORT:-5432}" PGUSER="$RESTORE_PGUSER" PGPASSWORD="$RESTORE_PGPASSWORD" PGDATABASE="$RESTORE_PGDATABASE"
pg_restore --no-owner --no-acl --exit-on-error -d "$PGDATABASE" "$TMP/dump" || falhar "pg_restore falhou (o PG efêmero precisa estar vazio)"
log "pg_restore concluído"

# O manifesto entra por stdin como tabela temporária; invariantes.sql compara.
{
  echo 'CREATE TEMP TABLE manifesto (tipo text, nome text, valor text);'
  echo 'COPY manifesto FROM STDIN;'
  cat "$TMP/manifesto"
  echo '\.'
  cat "$DIR_SCRIPTS/invariantes.sql"
} | psql -qX -v ON_ERROR_STOP=1 || falhar "invariantes violadas (veja acima)"

log "ok — invariantes verificadas; tempo total (RTO) = $((SECONDS - INICIO))s"

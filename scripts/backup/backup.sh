#!/usr/bin/env bash
# Uma execução de backup (task 0043, ADR 0013): pg_dump -Fc | age | rclone rcat,
# tudo em stream — o dump em claro NUNCA toca o disco (só FIFOs e arquivos de
# poucos bytes no tmp). Nunca imprime URL de heartbeat nem credenciais.
#
# Env (obrigatórias): BACKUP_AGE_RECIPIENT (age1…, só a chave PÚBLICA),
#   BACKUP_DESTINO (remote:bucket do rclone; o remote vem de RCLONE_CONFIG_*),
#   PGHOST/PGPORT/PGUSER/PGPASSWORD/PGDATABASE (role morfeu_backup).
# Env (opcionais): BACKUP_HEARTBEAT_URL (vazia = sem ping),
#   BACKUP_TAMANHO_MINIMO (bytes do dump cifrado; padrão 1024).
set -euo pipefail

TAMANHO_MINIMO="${BACKUP_TAMANHO_MINIMO:-1024}"
HEARTBEAT="${BACKUP_HEARTBEAT_URL:-}"
SUCESSO=0
INICIO=$SECONDS
TMP=""
SNAP_ABERTO=0
# Remotes vêm só de RCLONE_CONFIG_* (env): sem arquivo de config, sem aviso.
export RCLONE_CONFIG="${RCLONE_CONFIG:-/dev/null}"
RCLONE_OPTS=(--contimeout 30s --timeout 5m --low-level-retries 5)

log() { echo "backup: $*"; }

ping_heartbeat() { # sufixo ("" = sucesso, "/fail" = falha) — nunca ecoa a URL
  [ -n "$HEARTBEAT" ] || return 0
  if ! curl -fsS -m 15 --retry 3 -o /dev/null "${HEARTBEAT%/}$1" 2>/dev/null; then
    log "aviso: ping de heartbeat não confirmado"
  fi
  return 0
}

encerrar() {
  local rc=$?
  trap - EXIT
  if [ "$SNAP_ABERTO" = 1 ]; then kill "${SNAP_PID:-0}" 2>/dev/null || true; fi
  if [ -n "$TMP" ]; then rm -rf "$TMP"; fi
  if [ "$rc" -eq 0 ] && [ "$SUCESSO" = 1 ]; then
    ping_heartbeat ""
  else
    [ "$rc" -ne 0 ] || rc=1
    log "FALHA (código $rc, $((SECONDS - INICIO))s)"
    ping_heartbeat "/fail"
  fi
  exit "$rc"
}
trap encerrar EXIT

falhar() { echo "backup: ERRO: $*" >&2; exit 1; }

# --- pré-condições -----------------------------------------------------------
[ -n "${BACKUP_AGE_RECIPIENT:-}" ] || falhar "BACKUP_AGE_RECIPIENT não definido"
case "$BACKUP_AGE_RECIPIENT" in age1*) ;; *) falhar "BACKUP_AGE_RECIPIENT deve ser uma chave pública age (age1…)" ;; esac
[ -n "${BACKUP_DESTINO:-}" ] || falhar "BACKUP_DESTINO não definido (ex.: backup:meu-bucket)"
DESTINO="${BACKUP_DESTINO%/}"
TMP="$(mktemp -d)"

# --- banco certo e migrations limpas ----------------------------------------
existe="$(psql -qAtX -v ON_ERROR_STOP=1 -c "SELECT to_regclass('public.schema_migrations') IS NOT NULL")" ||
  falhar "não foi possível consultar o banco"
[ "$existe" = "t" ] || falhar "banco sem schema_migrations (vazio ou errado) — backup recusado"

# --- snapshot único: contagens do manifesto e pg_dump veem o mesmo estado ----
coproc SNAP { psql -qAtX -v ON_ERROR_STOP=1; }
SNAP_ABERTO=1

consultar() { # sql -> linhas do resultado (até o sentinela) na saída padrão
  local linha
  printf '%s;\nSELECT %s;\n' "$1" "'__FIM__'" >&"${SNAP[1]}"
  while IFS= read -r -t 120 linha <&"${SNAP[0]}"; do
    [ "$linha" = "__FIM__" ] && return 0
    printf '%s\n' "$linha"
  done
  return 1
}

consultar "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY" >/dev/null || falhar "não foi possível abrir o snapshot"
# (consultar não pode rodar em $(...): subshells não herdam os FDs do coproc.)
consultar "SELECT pg_export_snapshot()" >"$TMP/r" || falhar "snapshot não exportado"
SNAPSHOT="$(head -n1 "$TMP/r")"
[[ "$SNAPSHOT" =~ ^[0-9A-Fa-f-]+$ ]] || falhar "snapshot não exportado"

consultar "SELECT version::text || ' ' || dirty::text FROM schema_migrations" >"$TMP/r" || falhar "falha ao ler schema_migrations"
versao="$(cat "$TMP/r")"
[ "$(printf '%s\n' "$versao" | grep -c .)" = 1 ] || falhar "schema_migrations sem exatamente uma versão"
[ "${versao#* }" = "false" ] || falhar "schema_migrations suja (migration pela metade) — backup recusado"

{
  printf 'versao\tschema_migrations\t%s\n' "${versao% *}"
  consultar "SELECT 'tabela' || E'\\t' || tablename || E'\\t' || (xpath('/row/c/text()', query_to_xml(format('select count(*) as c from %I.%I', schemaname, tablename), false, true, '')))[1]::text FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename"
} >"$TMP/manifesto" || falhar "falha ao contar as tabelas"

# --- nomes ------------------------------------------------------------------
AGORA="$(date -u +%Y%m%dT%H%M%SZ)"
NOME="morfeu-$AGORA"
if [ "$(date -u +%u)" = 7 ]; then PREFIXO="semanal"; else PREFIXO="diario"; fi

# cifrar_e_enviar <arquivo-remoto> <comando-que-escreve-o-claro...>
# Calcula sha256 e tamanho do CIFRADO no stream (FIFOs, nada em disco).
# Resultado em $TMP/sha e $TMP/tam.
cifrar_e_enviar() {
  local remoto="$1" status=0 p1 p2
  shift
  rm -f "$TMP/sha" "$TMP/tam"
  mkfifo "$TMP/f-sha" "$TMP/f-tam"
  sha256sum <"$TMP/f-sha" | cut -d' ' -f1 >"$TMP/sha" &
  p1=$!
  wc -c <"$TMP/f-tam" | tr -d ' ' >"$TMP/tam" &
  p2=$!
  "$@" | age -r "$BACKUP_AGE_RECIPIENT" | tee "$TMP/f-sha" "$TMP/f-tam" |
    rclone rcat "${RCLONE_OPTS[@]}" "$DESTINO/$PREFIXO/$remoto" || status=$?
  wait "$p1" || true
  wait "$p2" || true
  rm -f "$TMP/f-sha" "$TMP/f-tam"
  return "$status"
}

tamanho_remoto() { # arquivo -> bytes (vazio se ausente)
  rclone lsf "${RCLONE_OPTS[@]}" --format s --files-only --include "$1" "$DESTINO/$PREFIXO/" | head -n1
}

conferir_envio() { # arquivo
  local esperado remoto
  esperado="$(cat "$TMP/tam")"
  remoto="$(tamanho_remoto "$1")" || falhar "não foi possível listar o destino"
  [ -n "$remoto" ] || falhar "objeto $1 não encontrado no destino"
  [ "$remoto" = "$esperado" ] || falhar "tamanho de $1 no destino ($remoto) difere do enviado ($esperado)"
}

# --- dump cifrado -----------------------------------------------------------
cifrar_e_enviar "$NOME.dump.age" pg_dump -Fc --snapshot="$SNAPSHOT" || falhar "pg_dump | age | rclone falhou"
conferir_envio "$NOME.dump.age"
TAMANHO="$(cat "$TMP/tam")"
SHA="$(cat "$TMP/sha")"
[ "$TAMANHO" -ge "$TAMANHO_MINIMO" ] || falhar "dump cifrado com $TAMANHO bytes (< mínimo $TAMANHO_MINIMO)"
printf '%s  %s\n' "$SHA" "$NOME.dump.age" | rclone rcat "${RCLONE_OPTS[@]}" "$DESTINO/$PREFIXO/$NOME.sha256" ||
  falhar "envio do sha256 falhou"

# --- fim do snapshot --------------------------------------------------------
printf 'ROLLBACK;\n\\q\n' >&"${SNAP[1]}" || true
wait "$SNAP_PID" 2>/dev/null || true
SNAP_ABERTO=0

# --- globais (roles/tablespaces; sem senhas) --------------------------------
cifrar_e_enviar "$NOME.globals.age" pg_dumpall --globals-only --no-role-passwords --database="${PGDATABASE:-postgres}" ||
  falhar "pg_dumpall | age | rclone falhou"
conferir_envio "$NOME.globals.age"

# --- manifesto (só números e nomes de tabela) -------------------------------
rclone rcat "${RCLONE_OPTS[@]}" "$DESTINO/$PREFIXO/$NOME.manifesto" <"$TMP/manifesto" || falhar "envio do manifesto falhou"

log "ok objeto=$PREFIXO/$NOME.dump.age bytes=$TAMANHO duracao=$((SECONDS - INICIO))s"
SUCESSO=1

#!/usr/bin/env bash
# Preflight dos .env de produção (task 0042, RF02): falha se uma variável
# obrigatória estiver ausente/vazia, com placeholder conhecido ou, para
# senhas, com menos de 24 caracteres. NUNCA imprime valores — só o nome da
# variável e o motivo.
#
# Uso: scripts/preflight.sh .env.prod .env.observability   (make preflight)
# O perfil de cada arquivo vem do nome: contendo "observability" valida o
# .env.observability; qualquer outro valida o .env.prod.
set -u

MIN_SENHA=24
PLACEHOLDERS=(troque changeme senha postgres morfeu guest)
USUARIOS_PROIBIDOS=(postgres morfeu guest admin root)

PROD_SENHAS=(POSTGRES_PASSWORD PG_MIGRATOR_PASSWORD PG_APP_PASSWORD PG_PURGE_PASSWORD PG_BACKUP_PASSWORD PG_MONITOR_PASSWORD REDIS_PASSWORD RABBITMQ_DEFAULT_PASS)
PROD_USUARIOS=(POSTGRES_USER RABBITMQ_DEFAULT_USER)
OBS_SENHAS=(GF_SECURITY_ADMIN_PASSWORD DATA_SOURCE_PASS)
OBS_URLS=(DISCORD_WEBHOOK_URL HEALTHCHECKS_PING_URL)

falhas=0
declare -A valores

falhar() { # arquivo variável motivo
  echo "preflight: $1: $2 — $3" >&2
  falhas=$((falhas + 1))
}

# Lê KEY=VALUE sem executar nada (não usa source/eval).
carregar() {
  valores=()
  local linha chave valor
  while IFS= read -r linha || [ -n "$linha" ]; do
    linha="${linha%$'\r'}"
    case "$linha" in '' | '#'*) continue ;; esac
    linha="${linha#export }"
    chave="${linha%%=*}"
    [ "$chave" = "$linha" ] && continue
    valor="${linha#*=}"
    if [[ "$valor" =~ ^\"(.*)\"$ || "$valor" =~ ^\'(.*)\'$ ]]; then valor="${BASH_REMATCH[1]}"; fi
    valores["$chave"]="$valor"
  done <"$1"
}

tem_placeholder() { # valor
  local minusculo="${1,,}" p
  for p in "${PLACEHOLDERS[@]}"; do
    [[ "$minusculo" == *"$p"* ]] && return 0
  done
  return 1
}

checar_senha() { # arquivo variável
  local v="${valores[$2]:-}"
  if [ -z "$v" ]; then falhar "$1" "$2" "ausente ou vazia"; return; fi
  if tem_placeholder "$v"; then falhar "$1" "$2" "contém placeholder"; return; fi
  if [ "${#v}" -lt "$MIN_SENHA" ]; then falhar "$1" "$2" "menos de $MIN_SENHA caracteres"; fi
}

checar_usuario() {
  local v="${valores[$2]:-}" minusculo u
  if [ -z "$v" ]; then falhar "$1" "$2" "ausente ou vazia"; return; fi
  minusculo="${v,,}"
  for u in "${USUARIOS_PROIBIDOS[@]}"; do
    if [ "$minusculo" = "$u" ]; then falhar "$1" "$2" "valor padrão/placeholder"; return; fi
  done
  if [[ "$minusculo" == *troque* || "$minusculo" == *changeme* ]]; then falhar "$1" "$2" "contém placeholder"; fi
}

checar_url() {
  local v="${valores[$2]:-}"
  if [ -z "$v" ]; then falhar "$1" "$2" "ausente ou vazia"; return; fi
  if [[ "$v" != https://* ]] || tem_placeholder "$v"; then falhar "$1" "$2" "precisa ser uma URL https real"; fi
}

if [ "$#" -eq 0 ]; then
  echo "uso: $0 <arquivo.env>..." >&2
  exit 2
fi

for arquivo in "$@"; do
  if [ ! -r "$arquivo" ]; then
    falhar "$arquivo" "arquivo" "não encontrado ou ilegível"
    continue
  fi
  carregar "$arquivo"
  if [[ "$(basename "$arquivo")" == *observability* ]]; then
    for v in "${OBS_SENHAS[@]}"; do checar_senha "$arquivo" "$v"; done
    for v in "${OBS_URLS[@]}"; do checar_url "$arquivo" "$v"; done
  else
    for v in "${PROD_SENHAS[@]}"; do checar_senha "$arquivo" "$v"; done
    for v in "${PROD_USUARIOS[@]}"; do checar_usuario "$arquivo" "$v"; done
  fi
done

if [ "$falhas" -gt 0 ]; then
  echo "preflight: $falhas problema(s) — corrija antes do deploy" >&2
  exit 1
fi
echo "preflight: ok"

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
PROD_BACKUP_TEXTOS=(BACKUP_S3_REGION BACKUP_S3_BUCKET BACKUP_S3_ACCESS_KEY_ID BACKUP_S3_SECRET_ACCESS_KEY)
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

# Backup (task 0043): só a chave PÚBLICA do age na VM; credenciais S3 reais;
# endpoint e heartbeat (opcional) em https.
checar_backup() { # arquivo
  local v="${valores[BACKUP_AGE_RECIPIENT]:-}" t="${valores[BACKUP_HEARTBEAT_URL]:-}" e="${valores[BACKUP_S3_ENDPOINT]:-}" n
  if [ -z "$v" ]; then
    falhar "$1" BACKUP_AGE_RECIPIENT "ausente ou vazia"
  elif [[ "$v" != age1* ]] || [[ "${v,,}" == *troque* ]]; then
    falhar "$1" BACKUP_AGE_RECIPIENT "deve ser uma chave pública age (age1…) real"
  fi
  for n in "${PROD_BACKUP_TEXTOS[@]}"; do
    v="${valores[$n]:-}"
    if [ -z "$v" ]; then falhar "$1" "$n" "ausente ou vazia"; continue; fi
    [[ "${v,,}" == *troque* || "${v,,}" == *changeme* ]] && falhar "$1" "$n" "contém placeholder"
  done
  if [ -z "$e" ]; then
    falhar "$1" BACKUP_S3_ENDPOINT "ausente ou vazia"
  elif [[ "$e" != https://* ]] || [[ "${e,,}" == *troque* ]]; then
    falhar "$1" BACKUP_S3_ENDPOINT "precisa ser uma URL https real"
  fi
  # Heartbeat é opcional; presente, precisa ser https real.
  if [ -n "$t" ] && { [[ "$t" != https://* ]] || tem_placeholder "$t"; }; then
    falhar "$1" BACKUP_HEARTBEAT_URL "precisa ser uma URL https real (ou vazia)"
  fi
  return 0
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
  # Chave privada do age em qualquer .env = vazamento da chave do backup (ADR 0013).
  if grep -q 'AGE-SECRET-KEY' "$arquivo"; then
    falhar "$arquivo" "chave privada age" "AGE-SECRET-KEY encontrada — remova do .env; a privada fica fora da VM"
  fi
  carregar "$arquivo"
  if [[ "$(basename "$arquivo")" == *observability* ]]; then
    for v in "${OBS_SENHAS[@]}"; do checar_senha "$arquivo" "$v"; done
    for v in "${OBS_URLS[@]}"; do checar_url "$arquivo" "$v"; done
  else
    for v in "${PROD_SENHAS[@]}"; do checar_senha "$arquivo" "$v"; done
    for v in "${PROD_USUARIOS[@]}"; do checar_usuario "$arquivo" "$v"; done
    checar_backup "$arquivo"
  fi
done

if [ "$falhas" -gt 0 ]; then
  echo "preflight: $falhas problema(s) — corrija antes do deploy" >&2
  exit 1
fi
echo "preflight: ok"

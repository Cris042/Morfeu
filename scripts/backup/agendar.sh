#!/usr/bin/env bash
# Laço de agendamento (task 0043): dorme até BACKUP_HORA_UTC (padrão 06 =
# 03:00 em Brasília) e roda o backup.sh. Falha não derruba o laço; uma linha
# de log por execução (início, fim, resultado, tamanho, duração).
#
# Ganchos de teste (não usados em produção): BACKUP_AGENDAR_SEM_ESPERA=1 roda
# sem dormir; BACKUP_AGENDAR_MAX_EXECUCOES=N encerra após N execuções.
set -uo pipefail

HORA="${BACKUP_HORA_UTC:-06}"
SCRIPT="${BACKUP_SCRIPT:-/opt/backup/backup.sh}"
MAX="${BACKUP_AGENDAR_MAX_EXECUCOES:-0}"

[[ "$HORA" =~ ^[0-9]{1,2}$ ]] && [ "$((10#$HORA))" -le 23 ] || { echo "agendar: BACKUP_HORA_UTC inválida" >&2; exit 2; }

agora() { date -u +%Y-%m-%dT%H:%M:%SZ; }

segundos_ate_a_hora() { # sempre em (0, 86400]
  local h m s atual alvo
  h="$(date -u +%H)"
  m="$(date -u +%M)"
  s="$(date -u +%S)"
  atual=$((10#$h * 3600 + 10#$m * 60 + 10#$s))
  alvo=$((10#$HORA * 3600))
  echo $(((alvo - atual + 86399) % 86400 + 1))
}

execucoes=0
while true; do
  if [ "${BACKUP_AGENDAR_SEM_ESPERA:-0}" != 1 ]; then
    espera="$(segundos_ate_a_hora)"
    echo "agendar: $(agora) próxima execução em ${espera}s (${HORA}h UTC)"
    sleep "$espera"
  fi
  inicio=$SECONDS
  echo "agendar: $(agora) inicio"
  saida="$(bash "$SCRIPT" 2>&1)"
  rc=$?
  printf '%s\n' "$saida"
  bytes="$(printf '%s\n' "$saida" | sed -n 's/.*bytes=\([0-9]*\).*/\1/p' | tail -n1)"
  if [ "$rc" -eq 0 ]; then resultado=ok; else resultado=falha; fi
  echo "agendar: $(agora) fim resultado=$resultado codigo=$rc bytes=${bytes:-0} duracao=$((SECONDS - inicio))s"
  execucoes=$((execucoes + 1))
  if [ "$MAX" -gt 0 ] && [ "$execucoes" -ge "$MAX" ]; then exit 0; fi
  # Evita reexecutar no mesmo segundo se o backup terminar instantaneamente.
  [ "${BACKUP_AGENDAR_SEM_ESPERA:-0}" = 1 ] || sleep 1
done

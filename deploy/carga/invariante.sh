#!/usr/bin/env bash
# Invariante de assento (PRD 0045 RF08): roda deploy/carga/invariante.sql no
# banco de carga. Saída 0 = nenhuma violação; 1 = violações (listadas); 2 = erro
# ao consultar (nunca confunde "não consegui checar" com "íntegro").
#
# Por padrão fala com o Postgres da stack morfeu-carga via `docker compose exec`.
# PSQL_CMD substitui o comando psql (testes); PGOPTIONS é repassado (o smoke
# usa search_path para provar que uma violação plantada é acusada).
set -euo pipefail

# O compose exige os segredos só para interpolar (o container já está de pé);
# valores de enchimento evitam exigir os exports numa shell nova.
: "${JWT_SEGREDO:=interpolacao}" "${INGRESSO_TOKEN_SEGREDO_V1:=interpolacao}" "${STRIPE_WEBHOOK_SECRET:=interpolacao}"
export JWT_SEGREDO INGRESSO_TOKEN_SEGREDO_V1 STRIPE_WEBHOOK_SECRET

RAIZ="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SQL="$RAIZ/deploy/carga/invariante.sql"

if [[ -n "${PSQL_CMD:-}" ]]; then
  # shellcheck disable=SC2206 # PSQL_CMD é uma linha de comando, divisão por palavras é a intenção
  psql_cmd=(${PSQL_CMD})
else
  psql_cmd=(docker compose -p morfeu-carga --project-directory "$RAIZ"
    -f "$RAIZ/deploy/carga/docker-compose.carga.yml"
    exec -T -e "PGOPTIONS=${PGOPTIONS:-}" postgres psql -U postgres -d morfeu_carga)
fi

if ! saida="$("${psql_cmd[@]}" -X -v ON_ERROR_STOP=1 --csv <"$SQL")"; then
  echo "invariante: erro ao consultar o banco" >&2
  exit 2
fi

# CSV: 1ª linha é o cabeçalho; qualquer linha além dela é uma violação.
violacoes="$(printf '%s\n' "$saida" | tail -n +2)"
if [[ -n "$violacoes" ]]; then
  echo "invariante: VIOLAÇÕES (violacao,referencia,assento,quantidade):" >&2
  printf '%s\n' "$violacoes" >&2
  exit 1
fi
echo "invariante: OK (nenhuma violação)"

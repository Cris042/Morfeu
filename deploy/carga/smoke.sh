#!/usr/bin/env bash
# Smoke do ferramental de carga (PRD 0045 RF10) — manual, NÃO é gate de PR.
# Sobe a stack morfeu-carga, semeia (2×), valida os scripts k6 (`k6 inspect`) e
# roda cada cenário na taxa mínima (~20 s de carga no total), depois confere o
# invariante e prova que ele acusa uma violação plantada. Sai ≠ 0 em qualquer
# falha. Ao fim recria do zero (down -v) — SMOKE_MANTER=1 deixa a stack no ar.
#
# Uso (da raiz do repo): deploy/carga/smoke.sh
set -euo pipefail

RAIZ="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$RAIZ"

# Segredos efêmeros por execução — nada versionado.
export JWT_SEGREDO="${JWT_SEGREDO:-$(openssl rand -hex 32)}"
export INGRESSO_TOKEN_SEGREDO_V1="${INGRESSO_TOKEN_SEGREDO_V1:-$(openssl rand -hex 32)}"
export STRIPE_WEBHOOK_SECRET="${STRIPE_WEBHOOK_SECRET:-whsec_$(openssl rand -hex 16)}"

compose() {
  docker compose -p morfeu-carga --project-directory "$RAIZ" -f deploy/carga/docker-compose.carga.yml "$@"
}
psql_stack() { compose exec -T postgres psql -U postgres -X "$@"; }
passo() { printf '\n== %s\n' "$*"; }
falhar() { echo "SMOKE FALHOU: $*" >&2; exit 1; }

limpar() {
  local codigo=$?
  if [[ $codigo -ne 0 ]]; then
    echo "-- últimos logs do app:" >&2
    compose logs --tail 30 app >&2 || true
  fi
  if [[ "${SMOKE_MANTER:-0}" != "1" ]]; then
    compose --profile k6 down -v --remove-orphans >/dev/null 2>&1 || true
  fi
  exit "$codigo"
}
trap limpar EXIT

passo "1/7 recriando a stack (down -v + up --build)"
compose --profile k6 down -v --remove-orphans >/dev/null 2>&1 || true
compose up -d --build --wait

passo "2/7 aguardando a API (/health) e conferindo o modo de carga"
for _ in $(seq 1 60); do
  curl -fsS http://127.0.0.1:18080/health >/dev/null 2>&1 && break
  sleep 2
done
curl -fsS http://127.0.0.1:18080/health >/dev/null || falhar "API não ficou saudável"
curl -fsS http://127.0.0.1:18080/metrics | grep -qx 'morfeu_modo_loadtest 1' \
  || falhar "gauge morfeu_modo_loadtest != 1 com a flag ligada"

passo "3/7 seed: guarda do banco, 1ª execução e idempotência (2ª execução)"
if psql_stack -d postgres -v ON_ERROR_STOP=1 <deploy/carga/seed.sql >/dev/null 2>&1; then
  falhar "o seed NÃO recusou um banco sem sufixo _carga"
fi
echo "seed recusou o banco 'postgres' (esperado)"
consulta="SELECT (SELECT count(*) FROM filmes) || '/' || (SELECT count(*) FROM salas) || '/' || (SELECT count(*) FROM sessoes) || '/' || (SELECT count(*) FROM usuario)"
semear() { psql_stack -d morfeu_carga -v ON_ERROR_STOP=1 <deploy/carga/seed.sql >/dev/null; }
semear
contagem1="$(psql_stack -d morfeu_carga -tAc "$consulta")"
semear
contagem2="$(psql_stack -d morfeu_carga -tAc "$consulta")"
echo "filmes/salas/sessoes/usuarios: $contagem1 -> $contagem2"
[[ "$contagem1" == "$contagem2" ]] || falhar "o seed duplicou dados ao rodar 2×"

k6() { compose --profile k6 run --rm -T "$@"; }

passo "4/7 k6 inspect em todos os scripts"
for s in leitura checkout misto disputa; do
  k6 k6 inspect "/scripts/$s.js" >/dev/null
  echo "ok: $s.js"
done

passo "5/7 cenários na taxa mínima (~20 s de carga)"
k6 -e TAXA=10 -e DURACAO=4s k6 run --quiet /scripts/leitura.js
k6 -e PERFIL=rampa -e PASSOS=4 -e PASSO=1s -e RAMPA_DE=5 -e RAMPA_ATE=20 k6 run --quiet /scripts/leitura.js
k6 -e TAXA=5 -e DURACAO=5s k6 run --quiet /scripts/misto.js
k6 -e TAXA=2 -e DURACAO=3s -e DISPUTANTES=10 k6 run --quiet /scripts/disputa.js
k6 -e TAXA=2 -e DURACAO=4s k6 run --quiet /scripts/checkout.js

passo "6/7 invariante de assento"
deploy/carga/invariante.sh

passo "7/7 o invariante acusa uma violação plantada (schema descartável)"
psql_stack -d morfeu_carga -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
DROP SCHEMA IF EXISTS carga_selftest CASCADE;
CREATE SCHEMA carga_selftest;
-- Cópias SEM índices/CHECK: permitem plantar o que o banco real nunca deixaria.
CREATE TABLE carga_selftest.holds (LIKE public.holds INCLUDING DEFAULTS);
CREATE TABLE carga_selftest.ingressos (LIKE public.ingressos INCLUDING DEFAULTS);
CREATE TABLE carga_selftest.pedidos (LIKE public.pedidos INCLUDING DEFAULTS);
SQL
PGOPTIONS="-c search_path=carga_selftest" deploy/carga/invariante.sh >/dev/null \
  || falhar "o invariante acusou violação num schema vazio"
psql_stack -d morfeu_carga -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
INSERT INTO carga_selftest.holds (id, sessao_id, assento_codigo, dono_hash, status, expires_at, criado_em, atualizado_em)
SELECT gen_random_uuid(), 1, 'A1', decode(repeat('00', 32), 'hex'), 'ativo', now(), now(), now()
FROM generate_series(1, 2);
SQL
if PGOPTIONS="-c search_path=carga_selftest" deploy/carga/invariante.sh >/dev/null 2>&1; then
  falhar "o invariante NÃO acusou o assento com dois holds vivos"
fi
echo "invariante acusou a violação plantada (saída ≠ 0), como esperado"
psql_stack -d morfeu_carga -v ON_ERROR_STOP=1 -c 'DROP SCHEMA carga_selftest CASCADE' >/dev/null

passo "SMOKE OK"

# Carga — runbook (E12, task 0045)

Como rodar o teste de carga **local** (stack descartável + k6 em container). Veredito: `docs/carga/queries.md`. Execução e relatório: task 0046.

> **Nunca contra a URL pública/produção.** `lib.js` recusa qualquer `BASE_URL` que não seja `http://` com host do compose (sem ponto), `localhost`, loopback, rede privada ou `host.docker.internal`. O modo de carga só liga com banco `*_carga`, gateway/e-mail fakes e `AMBIENTE != producao` (o boot recusa o resto), e o alerta `morfeu-modo-loadtest` dispara se a flag estiver ligada onde não deveria.

## Pré-requisitos

- Docker + compose v2; nesta máquina, nada mais (o k6 é a imagem `grafana/k6:2.3.0@sha256:9c2d…` do compose).
- Sempre da **raiz do repo**. Atalhos usados abaixo:

```bash
export JWT_SEGREDO=$(openssl rand -hex 32)
export INGRESSO_TOKEN_SEGREDO_V1=$(openssl rand -hex 32)
export STRIPE_WEBHOOK_SECRET=whsec_$(openssl rand -hex 16)
C="docker compose -p morfeu-carga --project-directory . -f deploy/carga/docker-compose.carga.yml"
```

(os três segredos são efêmeros e só existem na sua sessão; nada é versionado.)

## Subir, semear e conferir

```bash
$C up -d --build --wait                 # PG morfeu_carga, Redis, RabbitMQ, app (MORFEU_LOADTEST=1)
$C exec -T postgres psql -U postgres -d morfeu_carga -v ON_ERROR_STOP=1 < deploy/carga/seed.sql
curl -s http://127.0.0.1:18080/metrics | grep morfeu_modo_loadtest     # tem que ser 1
```

- API em `127.0.0.1:18080` (porta própria, não colide com o dev). Limites do compose: app 2 vCPU/2 GB (`GOMEMLIMIT=1600MiB`), PG 1 GB, k6 2 vCPU/1 GB.
- Seed: 50 filmes (10 das migrations + 40 sintéticos), 20 salas × 200 assentos, **1120 sessões** futuras (7 dias × 8 por sala, sem conflito de horário) = 224 000 assentos, 200 usuários `carga{n}@example.test`. Parâmetros por `-v` (ver o cabeçalho do `seed.sql`). **Idempotente** (rodar 2× não duplica) e **recusa** banco sem sufixo `_carga`. Consumo típico: a disputa gasta 1 assento por iteração; o checkout, 1 por fluxo — mantenha ≥ 10× de folga (aumente `-v dias=`/`-v salas=` para runs longos).
- Os usuários do seed usam um **hash Argon2id fixo** de uma senha aleatória **já descartada**: servem de massa de dados, ninguém loga com eles (a carga não faz login). Para uma conta com login, use `POST /auth/registro`. Para outro hash no seed: crie temporariamente `internal/identidade/zz_hash_test.go` com `gerarHash("sua-senha", ParametrosArgon2Padrao)` num teste (`go test ./internal/identidade -run NomeDoTeste -v`), cole o PHC (`$argon2id$v=19$…`) no `INSERT INTO usuario` do `seed.sql` e **apague o arquivo temporário**. Nunca versione a senha.

## Rodar os cenários

Cada cenário é um script k6 em `deploy/carga/k6/`; parâmetros por env (`-e`). Padrões = smoke de ~20 s.

```bash
K="$C --profile k6 run --rm"          # + -e ... <serviço> run /scripts/<script>.js
$K -e TAXA=50 -e DURACAO=2m k6 run /scripts/leitura.js
```

| Cenário | Comando (após `$K`) | Notas |
|---|---|---|
| **Baseline quente** | `-e TAXA=50 -e DURACAO=2m k6 run /scripts/leitura.js` | rode antes um aquecimento curto (mesmo comando, `DURACAO=30s`) |
| **Baseline frio** | `$C exec redis redis-cli FLUSHALL` e então o mesmo comando | cache vazio no início — se houver stampede no cartaz, é aqui que aparece |
| **Rampa 50→400** | `-e PERFIL=rampa -e RAMPA_DE=50 -e RAMPA_ATE=400 -e PASSOS=7 -e PASSO=1m k6 run /scripts/leitura.js` | `ramping-arrival-rate`; achar o ponto onde o p95 ou o erro quebra |
| **Patamar do SLO** | `-e TAXA=300 -e DURACAO=10m -e VUS_PRE=200 -e VUS_MAX=600 k6 run /scripts/leitura.js` | **3×**, mediana; janela do veredito = últimos 9 min |
| **Misto 90/10** | `-e TAXA=50 -e DURACAO=10m k6 run /scripts/misto.js` | `TAXA` = iterações/s totais: 0,9·TAXA leituras e 0,1·TAXA checkouts (4 req cada) |
| **Disputa** | `-e TAXA=5 -e DURACAO=1m -e DISPUTANTES=20 k6 run /scripts/disputa.js` | cada iteração = 20 travas paralelas no MESMO assento: 1×201 + 19×409, zero 5xx (checks e threshold) |
| **Checkout fim a fim** | `-e TAXA=3 -e DURACAO=5m k6 run /scripts/checkout.js` | trava → pedido → `/__teste/pagar` → pedido pago; `checkout_fluxo_duration` = SLI do fluxo |
| **Soak** | `-e TAXA=30 -e DURACAO=30m k6 run /scripts/misto.js` | mesmo `misto.js`, só mais longo (o soak longo é da VM) |

Variáveis: `TAXA`, `DURACAO`, `VUS_PRE` (padrão 50), `VUS_MAX` (200), `DISPUTANTES` (20), `PERFIL`/`PASSOS`/`PASSO`/`RAMPA_DE`/`RAMPA_ATE` (rampa), `SESSOES_POR_FILME`/`MAX_SESSOES` (descoberta), `BASE_URL` (padrão `http://app:8080`).

Depois de **cada** execução:

```bash
deploy/carga/invariante.sh     # exit 0 = íntegro; 1 = violações listadas; 2 = erro ao consultar
```

O invariante (`deploy/carga/invariante.sql`) procura: assento com mais de um hold `ativo|convertido`, assento com mais de um ingresso `ativo`, pedido `pago` sem ingresso. É a fonte única (ADR 0006).

## Observabilidade (opcional, para o veredito)

Compose combinado com a stack existente (precisa do `.env.observability` — copie de `.env.observability.example`; ajuste `DATA_SOURCE_URI=postgres:5432/morfeu_carga?sslmode=disable` e `DATA_SOURCE_PASS` = a senha do PG da stack, `carga` por padrão):

```bash
$C -f docker-compose.observability.yml up -d --build --wait
```

Grafana em `127.0.0.1:3000`; Prometheus raspa `app:8080`. O compose de observabilidade usa `container_name` fixo (`morfeu-prometheus`…): pare a stack de observabilidade do dev antes. As queries do veredito estão em `docs/carga/queries.md`.

## Critérios de validade do run

Um run só vale se **todos**:

1. **Gerador ≤ ~70% de CPU** do limite dele (2 núcleos) durante o patamar (`queries.md` §5; ou `docker stats` na hora).
2. **`dropped_iterations = 0`** no resumo do k6 (o script já falha o threshold). Iteração descartada = o gerador não sustentou a taxa → run **inválido**, não "provisório": aumente `VUS_PRE`/`VUS_MAX` ou reduza a taxa.
3. `http_req_failed < 1%` e checks > 99,9% (guarda-corpo do script) **e** `morfeu_modo_loadtest = 1` (senão há 429 de rate limit contaminando).
4. Invariante de assento íntegro ao final.

Números locais são **indicativos** (gerador e app no mesmo notebook); o M5 oficial roda na VM com o gerador fora dela (checklist da E0c-CD).

## Limpeza

**Limpeza = recriar a stack** (nunca limpar tabelas na mão):

```bash
$C --profile k6 down -v --remove-orphans     # apaga banco, Redis e RabbitMQ da carga
```

## Smoke

`deploy/carga/smoke.sh` faz tudo isso na taxa mínima (~20 s de carga): recria a stack, semeia 2× (idempotência e guarda do banco), `k6 inspect` em todos os scripts, roda os cenários, confere o invariante e prova que ele acusa uma violação plantada. Manual — **não é gate de PR**. `SMOKE_MANTER=1` deixa a stack no ar ao final.

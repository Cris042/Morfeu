# Ambiente de desenvolvimento (WSL2) — comandos canônicos

> Fatos verificados nas tasks 0003/0004. **Consultar ANTES de redescobrir o ambiente na tentativa e erro** — cada item abaixo já custou uma sessão de debugging. Atualizar quando o ambiente mudar.

## Mapa do ambiente

| Ferramenta | Onde vive | Pegadinha |
|---|---|---|
| Go 1.25.0 | `~/.local/go/bin` (user-level) | **Fora do PATH de shells não-interativos** — exportar antes de usar |
| gcc | **não existe no WSL** | `-race` exige cgo → rodar via container `golang:1.25` |
| golangci-lint | **sem binário local** | rodar via imagem `golangci/golangci-lint` (config do repo é **v2** — exige golangci-lint 2.x; auditado com v2.12.2) |
| sqlc | via `go install` | sqlc 1.31.x exige Go ≥ 1.26 p/ compilar → **`GOTOOLCHAIN=auto`** (CI pina `v1.31.1`) |
| compose | `docker compose` (v2) | `docker-compose` hifenizado **não existe mais** (o Makefile ainda o referencia) |
| Repo | `/mnt/c/...` (NTFS) | I/O lento; locks de container Docker impedem `rm -rf` de diretórios — parar containers antes de limpar |

## Comandos canônicos

```bash
# Go local (build/vet/test SEM -race):
export PATH="$HOME/.local/go/bin:$PATH"
go build ./... && go vet ./...

# Suíte completa com -race (gcc só existe no container; testcontainers reais
# precisam do socket do Docker; Ryuk desabilitado no ambiente WSL):
docker run --rm -v "$PWD":/src -w /src \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v morfeu-gomodcache:/go/pkg/mod \
  -e TESTCONTAINERS_RYUK_DISABLED=true \
  golang:1.25 go test -race -tags=integration ./...

# Lint (imagem 2.x — a config v2 do repo NÃO roda em golangci-lint 1.x):
docker run --rm -v "$PWD":/src -w /src \
  golangci/golangci-lint:latest golangci-lint run ./...

# sqlc (gerar/validar — mesma versão pinada do CI):
GOTOOLCHAIN=auto go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
sqlc generate && sqlc vet

# Infra local (PG + Redis):
docker compose up -d
```

## SPA (`web/`, task 0017 — ADR 0009)

Node **24** via nvm (`web/.nvmrc`; `engines` recusa outra major). Dois processos em dev: a API Go na `:8080` e o Vite na `:5173`, que repassa `/api/*` para a API **removendo o prefixo** (o Echo não conhece `/api`).

```bash
cd web
nvm use            # lê .nvmrc (Node 24)
npm ci             # lockfile exato (package-lock.json versionado)
npm run dev        # http://localhost:5173 — exige a API em :8080
npm test           # vitest run (sem rede; fetch é dublê)
npm run lint && npm run typecheck && npm run build
npm audit --audit-level=high   # gate no web-ci
```

- A API precisa estar em `:8080` (`docker compose --profile app up -d` ou `go run ./cmd/morfeu -mode=api` com PG/Redis). O cookie `morfeu_carrinho` (`Path=/`) atravessa o proxy sem ajuste — verificado na task 0017.
- `npm install` em `/mnt/c` é lento (≈ 4–5 min na 1ª vez): I/O do Windows. Some quando o repo for para o ext4 do WSL (item 0 do roadmap).
- Nunca use `localStorage`/`sessionStorage` nem `fetch` fora de `src/api/` — o lint barra (PRD 0017 RF07).

### E2E (Playwright — task 0021)

```bash
# API em :8080 (compose --profile app, ou -mode=api com PG/Redis) + seed da sessão de teste:
docker compose exec -T postgres psql -U postgres -d morfeu -v ON_ERROR_STOP=1 < web/e2e/seed.sql
cd web && npx playwright install chromium   # 1ª vez (precisa das libs do sistema)
npm run e2e                                  # sobe o vite preview (proxy /api) e roda M3 + caminho feliz
```

- Sem Chrome/libs no WSL (caso desta máquina: faltam libasound/libxcomposite/libxrandr), use a imagem oficial: `docker run --rm --network host -v "$PWD/web":/work -w /work mcr.microsoft.com/playwright:v1.63.0-noble npx playwright test` (a imagem é grande — ~2 GB no 1º pull).
- No CI roda no job `E2E` (`.github/workflows/e2e.yml`), que sobe a stack pelo compose e anexa o relatório em falha.

## Checkout (tasks 0023–0025 — ADR 0010)

- Padrão: `MORFEU_GATEWAY=fake` — nenhuma cobrança real. O fake guarda as cobranças **em memória do processo**: use `-mode=all` (o compose já usa). Com API e worker separados, ou após reinício, o worker não conhece as cobranças do fake e a reconciliação/estorno ficam adiando.
- Stripe de teste: `MORFEU_GATEWAY=stripe` + `STRIPE_SECRET_KEY` (`rk_test_…`/`sk_test_…`) **e** `STRIPE_WEBHOOK_SECRET` — exigidos na API **e no worker** (o boot recusa sem eles). Webhooks locais: `docker compose --profile app --profile stripe up` e copie o `whsec_` de `docker compose logs stripe-cli`.
- As tarefas da saga (reconciliação + estornos + cobranças abertas de expirados) rodam a cada 1 min em `-mode=worker|all`; a limpeza da outbox publicada (> 7 dias), a cada 1 h.

### E-mail do ingresso (task 0028)

- O e-mail de confirmação sai pelo **fake** até a task 0029 (Resend): nada é enviado; o consumidor monta o HTML + os QRs normalmente.
- **Fixe `INGRESSO_TOKEN_SEGREDO_V1` no `.env` de dev** (≥ 32 bytes, ex.: `openssl rand -base64 32`): sem ele cada processo gera um segredo aleatório — links de e-mail deixam de valer após reiniciar e, com `api` e `worker` separados, não batem entre si (a página `/i/{id}.{token}` do E8 valida na API).
- `BASE_URL_PUBLICA` (padrão `http://localhost:5173`) é a origem dos links do e-mail; em produção precisa ser https.

### Pagamento de teste (task 0031)

- Com `MORFEU_GATEWAY=fake` **e** `STRIPE_WEBHOOK_SECRET` definido (qualquer valor de teste), a API registra `POST /__teste/pagar/{pedido_id}`: monta um `payment_intent.succeeded`, assina com o segredo do webhook e passa pelo mesmo `Verificar` + pivô da rota real (pedido → pago, ingressos, outbox, e-mail). É o "Pagar (teste)" do SPA em modo fake e do E2E do M4.
- A rota **não existe** com o gateway Stripe, e o boot recusa o gateway fake com `AMBIENTE=producao` (duas travas).

### Replay da DLQ (task 0027)

Só pelo shell (nenhuma rota HTTP). Liste antes com `-dry-run` (nada é publicado; as mensagens voltam à DLQ):

```bash
# no compose local (na VM, o mesmo via `docker compose exec`)
docker compose --profile app exec app /app replay-dlq -fila notificacao.pedido_confirmado.dlq -dry-run
docker compose --profile app exec app /app replay-dlq -fila notificacao.pedido_confirmado.dlq -limite 10
```

- DLQs aceitas: `catalogo.filme_criado.dlq`, `notificacao.pedido_confirmado.dlq` e `notificacao.pedido_estornado.dlq` (lista fechada em `broker.FilasReplay`).
- Cada mensagem é republicada com confirm **antes** do ack, com o **mesmo `message_id`**: queda no meio duplica e o dedup do consumidor absorve; nunca perde. Mensagem que continua falhando volta à DLQ depois de 3 entregas (sem loop).
- A saída e o log trazem só contagens e `message_id` — nunca payload.

## Lições registradas (por que este arquivo existe)

- **Task 0003**: sessão inteira redescobrindo que Go não estava instalado (build da E0a nunca tinha compilado), que lint só roda via Docker e que locks de container travavam a limpeza de `internal/catalogo`.
- **Task 0004**: 3 commits de fix no CI por versão de toolchain (`GOTOOLCHAIN=auto` p/ sqlc, último patch do Go 1.25 p/ govulncheck, chave inválida no schema v2 do golangci).
- **Task 0017**: containers de outro projeto (`horus-dev`) podem ocupar 5432/5672 — para smoke do proxy, PG/Redis efêmeros em portas livres (`-p 127.0.0.1:55432:5432`) + API em `-mode=api` (não precisa do RabbitMQ).
- O smoke local do app usa `docker compose up -d` + `curl localhost:8080/health` (200 com PG+Redis no ar).

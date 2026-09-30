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

## Lições registradas (por que este arquivo existe)

- **Task 0003**: sessão inteira redescobrindo que Go não estava instalado (build da E0a nunca tinha compilado), que lint só roda via Docker e que locks de container travavam a limpeza de `internal/catalogo`.
- **Task 0004**: 3 commits de fix no CI por versão de toolchain (`GOTOOLCHAIN=auto` p/ sqlc, último patch do Go 1.25 p/ govulncheck, chave inválida no schema v2 do golangci).
- **Task 0017**: containers de outro projeto (`horus-dev`) podem ocupar 5432/5672 — para smoke do proxy, PG/Redis efêmeros em portas livres (`-p 127.0.0.1:55432:5432`) + API em `-mode=api` (não precisa do RabbitMQ).
- O smoke local do app usa `docker compose up -d` + `curl localhost:8080/health` (200 com PG+Redis no ar).

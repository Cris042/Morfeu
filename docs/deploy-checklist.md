# Checklist de deploy — hardening da VM (E0c-CD)

> Task 0042, RF09. Itens que **só podem ser feitos/verificados na VM** (doc.md §14.5). Cada item tem **um comando de verificação** e entra como **critério de aceite da E0c-CD**: a épica só fecha com todos marcados. O que dá para fechar sem a VM já está no repositório (compose de produção, preflight, senha do Redis, roles do PG — ver `docs/observabilidade.md`).

Antes de tudo: `make preflight` (nos dois `.env`) deve imprimir `preflight: ok`.

## Rede

- [ ] **NSG da Oracle: só 22, 80 e 443** de entrada (ingress rules da subnet/VNIC, nada de `0.0.0.0/0` em outras portas).
  Verificação (de **fora** da VM): `nmap -Pn -p 1-65535 <ip-publico>` lista exatamente 22, 80 e 443 como abertas; em especial **5432, 6379, 5672, 15672, 3000, 9090, 3100, 3200, 4317/4318 fechadas**.
- [ ] **UFW ativo, default deny** (defesa em profundidade, além do NSG; lembre que o Docker publica portas passando por cima do UFW — por isso o compose de produção só publica em `127.0.0.1`).
  Verificação: `sudo ufw status verbose` mostra `Default: deny (incoming)` e só 22/80/443 permitidos.
- [ ] **Nenhuma porta publicada fora do loopback no host.**
  Verificação: `docker ps --format '{{.Names}} {{.Ports}}' | grep -v '127.0.0.1' | grep -- '->'` não retorna nada (o Caddy da E0c-CD publica 80/443 — é a única exceção esperada).

## Acesso

- [ ] **SSH só por chave** (sem senha, sem login de root).
  Verificação: `sudo sshd -T | grep -E '^(passwordauthentication|permitrootlogin|pubkeyauthentication) '` → `passwordauthentication no`, `permitrootlogin no` (ou `prohibit-password`), `pubkeyauthentication yes`.
- [ ] **Forced command na chave do deploy** (a chave do CI só executa o script de deploy, sem shell).
  Verificação: em `~/.ssh/authorized_keys` a linha da chave de deploy começa com `command="…",no-pty,no-port-forwarding,no-agent-forwarding`; `ssh -i <chave-deploy> <vm> id` executa apenas o comando forçado (não devolve o `id`).
- [ ] Grafana e management do RabbitMQ só por **túnel SSH** (`ssh -L 3000:127.0.0.1:3000 …`).
  Verificação: `curl -sS -m 5 http://<ip-publico>:3000` e `…:15672` falham/expiram de fora.

## Segredos

- [ ] **`.env.prod` e `.env.observability` com permissão 600**, dono = usuário que roda o compose, fora de qualquer diretório servido.
  Verificação: `stat -c '%a %U' .env.prod .env.observability` → `600 <usuario>` nos dois.
- [ ] **Preflight verde** com os valores reais (nenhum placeholder, senhas ≥ 24 caracteres).
  Verificação: `make preflight` → `preflight: ok`.
- [ ] **Segredos fora do histórico e dos argumentos de processo.**
  Verificação: `ps -eo args | grep -i requirepass` não retorna a senha; `git log -p -S"$(grep ^REDIS_PASSWORD .env.prod | cut -d= -f2)"` não acha nada.

## Dados

- [ ] **Redis exige senha.**
  Verificação: `docker exec morfeu-redis redis-cli ping` → `NOAUTH Authentication required.`; com `REDISCLI_AUTH` → `PONG`.
- [ ] **Roles do PG criados e o app sem privilégio de apagar a trilha.**
  Verificação: `docker exec morfeu-postgres psql -U <admin> -d morfeu -c '\du'` lista `morfeu_migrator`, `morfeu_app`, `morfeu_purge`, `morfeu_backup`; `psql "$DATABASE_URL" -c 'DELETE FROM eventos_auditoria'` (como `morfeu_app`) → `permission denied`.
- [ ] **`REDIS_URL` com senha e `AMBIENTE=producao`** no `.env` do app (sem senha o boot recusa).
  Verificação: `docker compose logs app | grep -i 'REDIS_URL'` sem erro de configuração.

## Operação

- [ ] **Logs com rotação** (10 MB × 3 por container) e `restart: unless-stopped`.
  Verificação: `docker inspect --format '{{.HostConfig.LogConfig.Config}} {{.HostConfig.RestartPolicy.Name}}' $(docker ps -q)` mostra `max-file:3 max-size:10m` e `unless-stopped` em todos.
- [ ] **Alerta de disco chegando** (cobre `/`, onde moram os volumes do Docker).
  Verificação: Grafana → Alerting → "Disco acima de 80%" em estado `Normal` (e, no aceite, forçar um disparo de teste).

## Backup e restore (task 0043 — ADR 0013; runbook em `docs/backup.md`)

Aceite **real** da E0c-CD: o repositório prova o mecanismo (teste de ida e volta com a imagem real), mas bucket, credencial e restore de produção só existem na Oracle.

- [ ] **Bucket privado criado** (nunca público) com as regras de lifecycle **`diario/` = 7 dias** e **`semanal/` = 31 dias**.
  Verificação: console da Oracle → Object Storage → bucket → Lifecycle Rules mostra as duas regras; `rclone lsd <remote>:` não lista o bucket como público (`oci os bucket get --bucket-name <b> --query 'data."public-access-type"'` → `"NoPublicAccess"`).
- [ ] **Credencial do job sem delete/overwrite** (política IAM só com `OBJECT_CREATE`, `OBJECT_READ`, `OBJECT_INSPECT`/listagem no bucket — sem `OBJECT_DELETE` e sem `OBJECT_OVERWRITE`).
  Verificação: com a credencial do job, `rclone deletefile <remote>:<bucket>/diario/<objeto>` → `AccessDenied`/403; reenviar um objeto existente com o mesmo nome também falha. Se a Oracle não oferecer essa política, aplicar o **plano B** de `docs/backup.md` e registrar aqui.
- [ ] **Endpoint/região/path-style do S3 da Oracle conferidos** (`BACKUP_S3_ENDPOINT` no formato `https://<namespace>.compat.objectstorage.<região>.oraclecloud.com`).
  Verificação: o primeiro backup real (abaixo) conclui sem erro; se o SDK pedir outro estilo de endereçamento, ajustar o provider/env no compose.
- [ ] **Chave age gerada fora da VM** e guardada em **dois lugares** (gerenciador de senhas + cópia offline); só `age1…` no `.env.prod`.
  Verificação: `grep -c AGE-SECRET-KEY .env.prod` → `0` (e `make preflight` verde); o dono consegue ler a chave nos dois lugares.
- [ ] **Heartbeat do backup** (healthchecks.io, check próprio de período 24 h + tolerância) configurado em `BACKUP_HEARTBEAT_URL` e com alerta no canal do dono.
  Verificação: `docker compose logs backup` mostra a execução; o check fica verde após o primeiro backup; parar o container por > 24 h dispara o alerta.
- [ ] **Primeiro backup real** concluído.
  Verificação: `docker compose exec backup bash /opt/backup/backup.sh` → `backup: ok objeto=… bytes=…`; no bucket aparecem `.dump.age`, `.globals.age`, `.sha256` e `.manifesto` com tamanhos coerentes.
- [ ] **Primeiro restore real**, em máquina do dono com a chave, num PG efêmero destruído ao final — **RTO medido registrado aqui** (meta ~1 h, `doc.md` §7).
  Verificação: passo a passo de `docs/backup.md`; `restore.sh` termina com `invariantes verificadas` e imprime o tempo total. RTO medido: ______ (data: ______).
- [ ] **Restore mensal agendado** (lembrete recorrente do dono) e **perda da chave** ensaiada no runbook (o que fazer: novo par, novo backup, histórico antigo perdido).

## Teste de carga — M5 oficial (E12, task 0046; runbook em `docs/carga/runbook.md`)

Aceite **real** da E0c-CD: o relatório `docs/carga/2026-10-01-local.md` é **indicativo (local)** — gerador e app no mesmo notebook, hardware e disco diferentes da VM. O M5 oficial repete os **mesmos scripts e queries** na VM-alvo, com o **gerador fora dela**. Resultado vai para um relatório novo em `docs/carga/` (data do run, selo "oficial (VM)"), nunca por cima do local.

- [ ] **Stack de carga na VM, separada da produção**: subir `deploy/carga/docker-compose.carga.yml` + `docker-compose.observability.yml` (projeto `morfeu-carga`, banco `morfeu_carga`, `MORFEU_LOADTEST=1`) numa VM/janela **sem tráfego real** e **sem o compose de produção no ar** (o boot recusa `MORFEU_LOADTEST` com `AMBIENTE=producao`, banco sem sufixo `_carga` e gateway/e-mail reais). Limites do compose = os da VM-alvo (app 2 vCPU/2 GB).
  Verificação: `curl -s http://127.0.0.1:18080/metrics | grep morfeu_modo_loadtest` → `1`; o alerta "Modo de carga ligado" dispara (esperado) e **volta a `Normal`** ao derrubar a stack (`down -v`).
- [ ] **Gerador fora da VM** (outra máquina na mesma região/rede, k6 `2.3.0`, ≥ 2 vCPU livres; imagem do `lib.md` com digest). O alvo é a rede privada ou um túnel — `lib.js` só aceita `http://` com host do compose, loopback ou rede privada, **nunca** a URL pública.
  Verificação: `docker stats`/cAdvisor do gerador ≤ ~70% de CPU durante o patamar e `dropped_iterations = 0` (senão o run é **inválido** e refeito — `docs/carga/runbook.md`, "Critérios de validade").
- [ ] **Patamar do SLO — leitura 300 req/s × 10 min, 3 vezes**: `-e TAXA=300 -e DURACAO=10m k6 run /scripts/leitura.js`. Veredito **server-side** pelas queries de `docs/carga/queries.md` (janela = últimos 9 min de cada patamar, UTC registrado) e **mediana das 3**: p95 < 300 ms nas 3 rotas do SLO (cartaz, sessões, ocupação) e erro 5xx < 1% (sem contar 409).
  Verificação: tabela das 3 execuções no relatório oficial; `invariante.sh` com exit 0 após cada uma.
- [ ] **Demais cenários na VM**: baseline quente/frio (10 req/s), ramp até o joelho, misto 90/10 × 10 min, disputa (≤ 1 vencedor por assento, zero 5xx) e checkout fim a fim (SLI), cada um seguido de `deploy/carga/invariante.sh` (exit 0).
  Verificação: relatório oficial com a mesma tabela do local (req/s alcançado, p50/p95/p99 server e client, erro, dropped, CPU do gerador).
- [ ] **Soak (≥ 30 min, idealmente horas) com o RSS real do Alloy** — o local não representa a VM: memória do app (`GOMEMLIMIT`), goroutines, pool do PG, filas do RabbitMQ e **RSS do Alloy** (limite de 512 MB do compose com tail sampling) sem crescimento monotônico.
  Verificação: queries §4 de `docs/carga/queries.md`; nenhum alerta além do "Modo de carga" e do Watchdog.
- [ ] **Recalibrar o Argon2 na VM** (`ARGON2_MEMORIA_KIB`/`ARGON2_ITERACOES`/`ARGON2_PARALELISMO` — padrão OWASP 19 MiB/t=2/p=1): medir o tempo de um login (`POST /auth/login`, conta criada via `/auth/registro`; o seed de carga **não** tem login) sob a carga de leitura do patamar; alvo p95 de login < 500 ms com o app a 2 vCPU. Se estourar, reduzir a concorrência de hash (ou subir parâmetros só se sobrar folga) e registrar os valores escolhidos aqui. Hashes antigos continuam válidos (a verificação lê os parâmetros do próprio hash).
  Verificação: valores finais anotados aqui: memória ______ KiB, iterações ______, paralelismo ______ (data: ______); p95 de login medido: ______ ms.
- [ ] **Resultado do M5 oficial registrado**: p95 mediano a 300 req/s = ______ ms (SLO < 300 ms), erro = ______ % (SLO < 1%), joelho do ramp = ______ req/s. Se o SLO não fechar, abrir task de ajuste com a evidência (nenhuma mudança estrutural sem ADR).

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

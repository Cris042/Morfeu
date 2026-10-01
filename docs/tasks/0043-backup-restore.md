# Task 0043 — Backup cifrado e restore validado (E11, T2)

- **Data:** 2026-10-01
- **Status:** concluída (auditoria APROVADA em 2026-10-01)
- **Branch:** `feature/0043-backup-restore`
- **PRD:** docs/prd/0043-backup-restore.md
- **Item do roadmap:** E11 — Hardening + backup (2ª de 3). Refinamento: `docs/refinamentos/E11-hardening-backup.md` §T2; ADR 0013.

## Objetivo

Cumprir o RPO 24 h / RTO ~1 h do `doc.md` §7: dump noturno cifrado com `age` para o Object Storage, retenção pelo bucket, heartbeat e restore com invariantes.

## Escopo

Imagem e scripts de backup/agendamento/restore; serviço no compose de prod; preflight das variáveis de backup; testes de ida e volta e matriz de erro; runbook (restore, perda de chave, lifecycle, IAM sem delete, LGPD); `lib.md`.

## Fora de escopo

Bucket, credencial e lifecycle reais na Oracle e o primeiro restore real (aceite da E0c-CD); PITR; backup do Redis/RabbitMQ.

## Arquivos esperados

~22 (lista no PRD).

## Dependências esperadas

`age` 1.2.1, `rclone` 1.75.1 (release oficial), `postgresql16-client` 16.15 (pacotes Alpine 3.22.6, só na imagem de backup).

## Critérios de aceite

Ver PRD (CA01–CA08).

## Riscos

- Backup quebrado em silêncio → heartbeat só após upload verificado + teste de ida e volta no CI.

## Estimativa de impacto

Médio: serviço novo isolado; o binário do app não muda.

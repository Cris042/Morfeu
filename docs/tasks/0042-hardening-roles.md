# Task 0042 — Hardening do compose, senha do Redis e roles do PG (E11, T1)

- **Data:** 2026-10-01
- **Status:** em implementação
- **Branch:** `feature/0042-hardening-roles`
- **PRD:** docs/prd/0042-hardening-roles.md
- **Item do roadmap:** E11 — Hardening + backup (1ª de 3). Refinamento: `docs/refinamentos/E11-hardening-backup.md` §T1; ADR 0013.

## Objetivo

Fechar o que dá do `doc.md` §14.5 sem a VM: compose de produção sem portas públicas e com senhas obrigatórias, Redis com senha e o banco com menor privilégio.

## Escopo

`docker-compose.prod.yml`; preflight do `.env`; senha do Redis (app, exporter, healthcheck); roles `morfeu_migrator`/`app`/`purge`/`backup` (init + migration 018 + `DATABASE_MIGRATE_URL`/`DATABASE_PURGE_URL`); testes estáticos e de integração; checklist da E0c-CD.

## Fora de escopo

App e Caddy no compose de prod, NSG/SSH/forced command (E0c-CD); ACL do Redis; backup (0043); limpeza (0044).

## Arquivos esperados

26 (lista no PRD).

## Dependências esperadas

Nenhuma nova.

## Critérios de aceite

Ver PRD (CA01–CA07).

## Riscos

- Grant esquecido deixa o app sem acesso em produção → default privileges + teste com o role do app.

## Estimativa de impacto

Médio: boot do app (URLs de banco e Redis), migration de permissões e compose novo; dev/CI inalterados pelo fallback.

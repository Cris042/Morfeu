# Refinamento — Épico E3 (Sessões e salas) — 2026-09-29

Cerimônia por épico (roles.md §6.14): brief único → pareceres (`arquiteto`, `security`, `qa`, `sre-devops`, depois `backend-dev`) → debate → 2 perguntas escaladas e **respondidas pelo usuário no mesmo dia**.

## Pareceres

| Agente | Posição | Pontos-chave |
|---|---|---|
| arquiteto | seguir com ressalvas | duração por **porta síncrona na criação + snapshot** (`duracao_min`, `fim`) na sessão; `EXCLUDE USING gist` + btree_gist; layout `jsonb` validado em Go, sem tabela de assentos (código = função pura), imutável com sessão ativa; `preco_centavos > 0`; status `agendada|cancelada`; só API; `GET /filmes/{id}/sessoes` sem campos de filme + cache |
| security | seguir | escrita só operador; layout com limite de payload, schema estrito e teto 50×50, sem códigos duplicados; faixa de preço; **rejeitar sessão no passado**; log `operador_id`+ação+`sessao_id` sem PII |
| qa | seguir com ressalvas | só API; unit table-driven com tempo injetado (borda encostada, parcial, contida, outra sala, virada de dia, filme arquivado antes do conflito); 2 criações concorrentes → exatamente 1; layout e imutabilidade; contrato público estável; invalidação; regressão do catálogo |
| sre-devops | seguir | btree_gist já na `postgres:16-alpine`; `fim` persistido; timestamptz UTC (exibição na borda); `sessao_conflitos_total`; índice `(filme_id, inicio) WHERE status='agendada'`; cache das sessões futuras TTL 60–120 s + invalidação |
| backend-dev | seguir | T1 ~20–24 arquivos, T2 ~8–10; tradução 23P01→409 confinada ao pacote `db`; métrica sem custo extra; cache na T2 |

## Debate (divergências e resolução)

1. **Duração do filme sem ler `filmes`** — (a) porta síncrona × (b) projeção por evento × (c) snapshot. → **Consenso**: (a)+(c) — `sessao` define `FonteFilmes.DadosFilme(id)`; `catalogo` implementa; `main` injeta; a sessão grava `duracao_min` e `fim` na criação. Projeção descartada (exigiria evento de atualização inexistente + consumidor extra — §1 anti-overengineering). Snapshot também é o certo no domínio: editar o filme depois não move sessão agendada.
2. **Onde traduzir o 23P01** — backend-dev propôs método à mão no `db.go` gerado. → **Ajuste no debate**: arquivos do sqlc são regenerados; a tradução fica num arquivo **não gerado** do pacote `internal/sessao/db` (`erros.go`, o único que importa `pgconn`), exposto como `db.EhConflitoDeHorario(err)`; o domínio mapeia para `ErrConflitoHorario` e reconsulta o conflitante para o 409.
3. **Cache das sessões** — TTL × invalidação ativa. → **Consenso**: read-through + invalidação síncrona na escrita (mesmo padrão do cartaz, 0011), TTL 60 s como teto de defasagem — na T2.
4. **Intervalo de limpeza** → **Escalado**: usuário escolheu **20 minutos** (constante única do MVP).
5. **"Telas mínimas do operador"** → **Escalado** (muda o texto do roadmap): usuário escolheu **só API agora; UI do operador no E5/E9** junto com o SPA.

## Conclusão

2 tasks; nenhum ADR novo (0003/0005/0006 cobrem). Roadmap ajustado (telas → E5/E9) com o aval do usuário.

### Exigências por task

#### T1 — Escrita: salas, sessões, não-conflito (backoffice)

- Migration: `CREATE EXTENSION IF NOT EXISTS btree_gist` (comentário sobre privilégio em banco gerenciado); `salas (id IDENTITY, nome UNIQUE, layout jsonb, criado_em, atualizado_em)`; `sessoes (id IDENTITY, filme_id BIGINT NOT NULL REFERENCES filmes, sala_id REFERENCES salas, inicio timestamptz, duracao_min, fim timestamptz, preco_centavos int CHECK > 0, status CHECK IN ('agendada','cancelada'), criado_em, atualizado_em)`, `CHECK (fim > inicio)`, **`EXCLUDE USING gist (sala_id WITH =, tstzrange(inicio, fim, '[)') WITH &&) WHERE (status = 'agendada')`**, índices das FKs e `(filme_id, inicio) WHERE status='agendada'`.
  - Nota de ownership: a FK `sessoes.filme_id → filmes(id)` é integridade referencial declarada pelo `sessao` (o banco protege o vínculo; o módulo **não lê** `filmes` — a duração vem da porta).
- Layout: JSON `{fileiras, colunas, vaos:[{fileira,coluna}], pcd:[{fileira,coluna}]}` validado em Go, schema estrito (`DisallowUnknownFields`), corpo ≤ 64 KB, 1 ≤ fileiras ≤ 26 (letras A–Z), 1 ≤ colunas ≤ 50, posições dentro da grade, sem duplicata, PCD não pode estar em vão; códigos de assento determinísticos (`F7` = fileira F, coluna 7), função pura exportada para o E4. Layout **imutável** se houver ≥ 1 sessão `agendada` futura na sala (409).
- Sessão: `fim = inicio + duracao_min + 20 min` (constante `IntervaloLimpeza`), calculado em Go com relógio injetável; `inicio` no passado → 400; `preco_centavos` 100–100000; filme inexistente/arquivado → 422 (a porta devolve `ativo=false`/não encontrado); conflito → **409 com o horário da sessão conflitante**; `sessao_conflitos_total` incrementado.
- Rotas `/backoffice/salas` (POST, GET, PUT layout/nome) e `/backoffice/sessoes` (POST, GET, POST `/{id}/cancelar`) com o middleware de operador injetado; log `operador_id`+ação+id.
- Depguard `sessao-domain` (strict, espelha `catalogo-domain`); `pgconn` só em `internal/sessao/db/erros.go`.
- Testes: unit table-driven do cálculo de `fim` e da validação de layout/sessão (tempo injetado); integração: bordas via EXCLUDE (encostada permitida, parcial/contida rejeitada, outra sala permitida, cancelada libera horário), **2 criações concorrentes → exatamente 1**, imutabilidade do layout, matriz de autorização, porta real do catálogo (arquivado → 422).

#### T2 — Leitura: sessões públicas + cache + listagem do operador

- `GET /filmes/{id}/sessoes` (público): só sessões `agendada` com `inicio > agora`, ordenadas; campos `{id, sala_id, sala_nome, inicio, fim, preco_centavos}` — **nenhum campo de filme**; `GET /sessoes/{id}/mapa` (público): layout + códigos de assento (base do E4/E5).
- Cache read-through `sessao:filme:{id}:futuras` TTL 60 s + invalidação síncrona em criar/cancelar; métrica hit/miss via log/contador existente.
- Listagem do operador com filtros simples (data, sala).
- Testes: contrato (sem campos de filme), invalidação verificada no Redis, regressão do cartaz.

### Exigências transferidas

- **E4**: `assento_codigo` usa a função de códigos do `sessao`; hold só em sessão `agendada` e futura.
- **E5/E9**: telas do operador (salas, sessões, filmes) nascem com o SPA.
- **E9**: cancelar sessão com ingressos vendidos (estorno) e trilha de auditoria formal.

### Perguntas escaladas ao usuário (respondidas em 2026-09-29)

1. Intervalo de limpeza → **20 minutos**.
2. Telas do operador → **só API no E3; UI no E5/E9**.

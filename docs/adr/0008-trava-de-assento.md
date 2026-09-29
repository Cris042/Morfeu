# ADR 0008 — Trava de assento: PostgreSQL com índice único parcial, expiração lazy e sweeper

- **Status:** aceito
- **Data:** 2026-09-29
- **Task/PRD relacionados:** refinamento E4 (`docs/refinamentos/E4-reserva.md`), tasks 0015/0016, `doc.md` fluxo crítico 2

## Contexto

O fluxo crítico 2 do `doc.md` exige que, quando dois clientes disputam o mesmo assento da mesma sessão, **exatamente um** vença. Isso vale sob concorrência real e sem depender do Redis. A descoberta já tinha fixado o PostgreSQL como fonte da verdade da trava (o usuário reverteu Redis após consenso 4/4), mas deixou o desenho como "candidata a ADR". O refinamento do E4 fechou os detalhes: como um hold expirado que ainda consta como `ativo` deixa de bloquear o índice, como travar N assentos sem deadlock e como limitar holds por dono sob concorrência. Essa decisão é reusada no E6: os ingressos repetem a invariante e o pedido converte holds.

## Escopo

Cobre: o modelo de dados da trava, o mecanismo de exclusão mútua, a expiração, o roubo de hold expirado, o lote de assentos, o teto por dono e o sweeper. Não cobre: o token de carrinho e o transporte (PRD 0015), o cache de ocupação (PRD 0016), os ingressos e a conversão no pedido (E6).

## Decisão

**A trava é uma linha em `holds`, protegida por `UNIQUE (sessao_id, assento_codigo) WHERE status = 'ativo'`. Há um único caminho de escrita: o upsert que rouba atomicamente um hold vencido. A expiração é avaliada de forma lazy por `expires_at`, e um sweeper faz só a higiene.**

- **Criar/roubar:** `INSERT ... ON CONFLICT (sessao_id, assento_codigo) WHERE status = 'ativo' DO UPDATE SET <novo id, novo dono, novo prazo> WHERE holds.expires_at <= $agora RETURNING id`. Sem linha no `RETURNING` = hold vivo de outro dono → 409 `assento_indisponivel` (resultado esperado, não é erro de SLO). O índice único resolve o INSERT concorrente e o lock de linha resolve o roubo concorrente. Não há pré-check.
- **Lote de N assentos (1–6):** uma TX, com upserts **em ordem crescente de código** (ordem global → sem espera circular entre lotes sobrepostos). Qualquer recusa desfaz a TX inteira (tudo ou nada).
- **Teto por dono:** `pg_advisory_xact_lock` com chave derivada do dono como 1ª instrução da TX; a contagem de holds vivos e a inserção ficam serializadas só para aquele dono.
- **Expiração:** TTL 10 min + 1 extensão de 10 min (`UPDATE` com guarda `extensoes_usadas = 0`). "Vivo" é sempre `status = 'ativo' AND expires_at > agora` e nunca depende do sweeper.
- **Sweeper:** processo no worker, a cada 60 s, marca `expirado` em lotes sob `pg_try_advisory_xact_lock` e não emite evento. Se ele parar, a correção não é afetada: o índice continua ocupado, mas o upsert rouba o vencido.
- **Relógio:** `agora` é um parâmetro vindo do relógio da aplicação (injetável — ADR 0006), nunca o `now()` do banco.
- **Duas linhas de defesa (ADR 0005):** o aggregate valida em memória (lote, layout, extensão). O índice parcial e os `UPDATE ... WHERE` com guarda garantem a invariante sob concorrência real.

## Tecnologias ou padrões envolvidos

PostgreSQL 16: índice único parcial, `INSERT ... ON CONFLICT ... DO UPDATE ... WHERE`, advisory locks transacionais. Padrões: lease com expiração lazy, ordenação global de locks, aggregate + repository (ADR 0005).

## Impacto esperado

O módulo `reserva` nasce com a tabela `holds` (migration própria, índices parciais), um aggregate + repository manual (ADR 0005), uma porta síncrona para o `sessao` e um sweeper no worker (goroutine no `WaitGroup` do shutdown, como a limpeza de refresh). Não há serviço, dependência nem custo novo: tudo é PostgreSQL já existente. Na operação, a trava gera métricas de criados, indisponíveis e expirados, e o sweeper tem alerta de parada (PRD 0016). Na manutenção, os ingressos do E6 repetem o mesmo padrão de índice parcial, e qualquer mudança no caminho de escrita precisa manter verde o teste canônico de corrida.

## Alternativas consideradas e descartadas

- **Redis `SET NX PX`**: rápido, mas a trava deixaria de ser transacional com o pedido (E6), um restart ou uma eviction perde travas, e o dado ficaria em duas fontes da verdade. O usuário descartou isso na descoberta.
- **`SELECT ... FOR UPDATE` numa tabela de assentos pré-materializada**: exigiria materializar assento × sessão (o layout é jsonb, sem tabela de assentos — E3) e mantém o lock durante a TX. O índice parcial dá a mesma garantia sem essa tabela.
- **Advisory lock por assento**: invisível no schema, não sobrevive como estado (a trava precisa durar 10 min, não uma TX) e não serve de invariante declarada para auditoria.
- **Pré-check de expirado + INSERT (ou sweeper síncrono antes do INSERT)**: dois passos criam uma janela de corrida e mais código. O upsert com guarda faz isso numa instrução.
- **`ON CONFLICT DO NOTHING` puro** (texto original do `doc.md`): não liberaria o assento de um hold vencido até o sweeper rodar, e a expiração deixaria de ser lazy.

## Benefícios

- A invariante "no máximo 1 hold vivo por assento" fica declarada no schema, e o E6 a reaproveita para ingressos.
- A correção não depende do sweeper, do Redis nem de relógio distribuído.
- O 409 da corrida é determinístico e barato (o índice decide), sem retry nem lock explícito no caminho feliz.
- É testável com PG real e relógio injetado, sem sleep (N goroutines + barreira + query de invariante).

## Trade-offs

- Cada trava é uma escrita no PostgreSQL. O polling de ocupação precisa de cache para não virar leitura quente (PRD 0016).
- O roubo sobrescreve a linha vencida: o histórico de holds expirados que nunca viraram pedido se perde (aceito: não há requisito de auditoria sobre eles).
- A tabela cresce com holds terminais até existir purge (pendência transferida ao hardening).
- O advisory lock por dono serializa requisições paralelas do mesmo carrinho. Isso é aceitável: um humano não trava assentos em paralelo.

## Riscos

- **Deadlock entre lotes sobrepostos** — probabilidade baixa com a ordenação global; impacto: 500 intermitente (a mesma classe vista no E3). Mitigação: ordem crescente de código + teste de lotes cruzados concorrentes no CI ARM64.
- **Relógios divergentes entre instâncias** — probabilidade baixa (binário único numa VM); impacto: um roubo alguns ms antes/depois. Mitigação: NTP da VM; o prazo é de minutos.
- **Crescimento da tabela** — probabilidade média a longo prazo, impacto baixo. Mitigação: índices parciais só sobre `ativo`; purge no hardening.

## Estratégias para minimizar os trade-offs

- Cache de ocupação com TTL de 3 s e modelo reduzido (só códigos ocupados) para absorver o polling.
- Métricas de holds criados, indisponíveis e expirados, mais alerta de sweeper parado, para detectar degradação sem afetar a correção.
- Teste canônico de corrida (N=20, 10 rodadas) + roubo sob corrida + lotes cruzados como gate permanente.

## ADRs relacionados

- ADR 0002 (sqlc + pgx; transação via `WithTx`), ADR 0003 (a `reserva` não lê `sessoes`/`salas` — porta síncrona), ADR 0005 (aggregate + repository na `reserva`), ADR 0006 (PG real, relógio injetado, corrida canônica). Nenhum é substituído.

## Condições de reversão

Revisitar se: (a) a latência de escrita da trava sob carga (k6, E12) ficar acima do SLO; (b) surgir mais de uma instância da API com relógios não confiáveis; (c) o E6 exigir trava distribuída entre bancos (improvável: banco único).

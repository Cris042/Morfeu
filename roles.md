# roles.md — Governança Técnica do Projeto

> **Fonte da verdade** de todas as regras, agentes, skills e fluxos deste projeto.
> Cada regra está escrita uma única vez, aqui. Agentes, skills e o CLAUDE.md apenas referenciam este arquivo.
> Sempre que uma nova regra estrutural for definida, este arquivo DEVE ser atualizado.

---

## 1. Visão geral

Este projeto segue uma governança **multiagente**: agentes especializados colaboram para garantir padronização técnica, qualidade, segurança, observabilidade, manutenibilidade, baixo acoplamento e rastreabilidade completa do ciclo de desenvolvimento.

Toda decisão técnica relevante deve considerar **benefícios, riscos, trade-offs, impacto no negócio, custo operacional, manutenção futura e complexidade adicionada**. O objetivo é sempre **minimizar os trade-offs negativos** e evitar soluções desnecessariamente complexas (anti-overengineering).

**Importante — como os agentes atuam:** os subagentes do Claude Code são **consultivos**: analisam e retornam recomendações estruturadas. Quem executa o fluxo (edições, skills, git) é a sessão principal, seguindo as recomendações dos agentes. Não existe "agente que faz push".

---

## 2. Fluxo padrão de desenvolvimento

```
descoberta → roadmap → refinamento do épico → task → PRD → implementação → gate pré-push → push → PR → CI + revisão → merge
```

| Etapa | Produz | Consome | Regra-chave |
|---|---|---|---|
| 0. Descoberta | `doc.md`, `lib.md`, `docs/roadmap.md`, direção de design e ADRs iniciais | entrevista com o usuário + pareceres dos 5 agentes | todo projeto começa aqui (§6.15) |
| 1. Roadmap | `docs/roadmap.md` | objetivos do projeto | épicos, prioridades, riscos, ordem sugerida |
| 2. Refinamento do épico | `docs/refinamentos/E*-*.md` com exigências por task | épico, ADRs, brief pré-digerido | 5 agentes, **uma vez por épico** (§6.14) |
| 3. Task | `docs/tasks/NNNN-*.md` + branch | roadmap + refinamento do épico | escopo pequeno, máx. 30 arquivos (§6.3); trivial dispensa refinamento (§6.14.6) |
| 4. PRD | `docs/prd/NNNN-*.md` | exigências do refinamento do épico | just-in-time ao abrir a task, sem novos agentes (§6.2.5) |
| 5. Implementação | código + testes | PRD, ADRs | seguir padrões; atualizar `plan.md` (§6.11) |
| 6. Gate pré-push | scan de secrets limpo | diff da branch | §6.4.1 (gitleaks local; mecânicos rodam no CI — §6.4.2) |
| 7. PR | Pull Request | template de PR | rastreável até task e PRD (§6.5) |
| 8. CI + revisão | pipeline verde + revisão de julgamento | diff do PR | itens mecânicos no CI; julgamento em 1 passe (§6.4.2–3) |
| 9. Merge | branch integrada em `main` | CI verde + revisão aprovada | **nenhum merge sem o gate completo** (§6.4.1) |

---

## 3. Agentes

Definidos em `.claude/agents/`. Invocação: pedir explicitamente ("consulte o agente arquiteto sobre X") ou deixar o Claude delegar quando a description casar.

| Agente | Arquivo | Responsabilidade central | Quando acionar |
|---|---|---|---|
| **Arquiteto de Software** | `arquiteto.md` | decisões de arquitetura e análise contínua da sua integridade, conformidade com ADRs, **todos** os trade-offs, volumetria, tendências do ecossistema, domínio do negócio, anti-overengineering | decisões estruturais, escolha de tecnologia, refinamento de épico, revisão de julgamento, antes de qualquer ADR |
| **SRE / DevOps** | `sre-devops.md` | docker-compose, ambiente reproduzível, observabilidade, gargalos | infra local, métricas/logs/traces, performance, CI |
| **Security Engineer** | `security.md` | segurança da concepção à implementação, CVEs, secrets, authn/authz | novas dependências, endpoints, dados sensíveis, **sempre na auditoria** |
| **QA Engineer** | `qa.md` | estratégia de testes, cobertura mínima, idempotência | plano de testes do PRD, revisão de suíte, **sempre na auditoria** |
| **Backend Developer** | `backend-dev.md` | implementação seguindo ADRs, PRD e diretrizes dos demais agentes | executar a implementação da task |

**Todos os 5 agentes participam do refinamento de todo épico** (§6.14) — cada um emite parecer do seu domínio, uma vez por épico, antes dos PRDs das tasks.

**Modelos e effort por agente** (frontmatter em `.claude/agents/`): `backend-dev`, `qa`, `security` e `sre-devops` rodam em **Sonnet com effort `medium`** — a inteligência pesada do fluxo vive nas decisões (refinamento, PRD, ADRs), que chegam prontas a esses agentes; o **`arquiteto` herda o modelo da sessão principal** (é o agente de julgamento — decisões de arquitetura e trade-offs). Rotinas headless (hook do vault) rodam em **Haiku**. O `backend-dev` roda com **lista explícita de tools** no frontmatter (dieta de contexto por spawn — decisão de 2026-07-12); os demais já têm listas restritas. Alterar modelo/effort/tools de agente é mudança estrutural (§8).

---

## 4. Skills

### 4.1 Skills do fluxo (deste projeto, em `.claude/skills/`)

| Skill | Propósito | Obrigatória quando |
|---|---|---|
| `iniciar-projeto` | descoberta/kickoff: entrevista profunda + análise multiagente; gera `doc.md`, `lib.md` e `docs/roadmap.md` iniciais | todo início de projeto, antes do roadmap (§6.15) |
| `criar-task` | quebrar roadmap em tasks pequenas e rastreáveis | iniciar qualquer trabalho novo |
| `refinar-task` | cerimônia de refinamento: os 5 agentes debatem a task e convergem | entre a task e o PRD (§6.14) |
| `criar-prd` | detalhar a task antes de implementar | antes de toda implementação |
| `criar-adr` | registrar decisão arquitetural relevante | decisão estrutural **com autorização do usuário** |
| `criar-migration` | mudança estrutural de banco versionada | toda alteração de schema |
| `auditoria` | gate de qualidade pré-push (13 itens) | **antes de todo push** |
| `fluxo-git` | branches, commits, PR padronizados | todo trabalho com git |

### 4.2 Skills de apoio por agente (todas vendorizadas neste repositório)

| Agente | Skills de apoio |
|---|---|
| Arquiteto | `backend-patterns`, `design-system`, `architecture-decision-records`ᵛ, `api-design`ᵛ, `coding-standards`ᵛ, `golang-project-layout`ᵍ, `golang-context`ᵍ |
| SRE/DevOps | `docker-patterns`, `benchmark`, `postgres-patterns`, `github-ops`ᵛ (CI/releases), `deployment-patterns`ᵛ, `error-handling`ᵛ (resiliência), `golang-observability-opentelemetry`ᵍ |
| Security | `security-review`, `security-scan`ᵛ (audita a própria config `.claude/` — rodar após vendorizar skills ou alterar agents/hooks/MCP), `golang-safety`ᵍ |
| QA | `tdd-workflow`ᵛ, `e2e-testing`ᵛ, `browser-qa`, `benchmark`, `coding-standards`ᵛ (item 13 da auditoria), `golang-testing`ᵍ |
| Backend Dev | `backend-patterns`, `postgres-patterns`, `tdd-workflow`ᵛ, `database-migrations`, `error-handling`ᵛ, `api-design`ᵛ, `coding-standards`ᵛ, `ui-ux-pro-max` (tarefas de UI), `golang-concurrency`ᵍ, `golang-context`ᵍ, `golang-error-handling`ᵍ, `golang-database`ᵍ, `golang-safety`ᵍ, `golang-lint`ᵍ, `golang-observability-opentelemetry`ᵍ |
| fluxo-git (skill) | `git-workflow`ᵛ, `github-ops`ᵛ |

**Skills Goᵍ** (vendorizadas em 2026-07-07, após definição da stack): 8 de [samber/cc-skills-golang](https://github.com/samber/cc-skills-golang) + 1 de [bobmatnyc/claude-mpm-skills](https://github.com/bobmatnyc/claude-mpm-skills) (a de OTel-Golang prevista na §4.3). **Em conflito entre uma skill Go e os ADRs 0003–0005, os ADRs prevalecem** — as skills são conhecimento de apoio, não regra. Origens e commits registrados no `lib.md`. A skill `jpa-patterns` (JPA/Spring, herdada do template) foi removida na mesma data — stack decidida é Go.

**Todas as skills de apoio estão vendorizadas em `.claude/skills/`** — o repositório é autossuficiente ao clonar (decisão de 2026-07-04; origem e créditos registrados no `lib.md`). O marcador ᵛ é histórico (primeiro lote vendorizado do ECC). Única exceção: `ui-ux-pro-max` é plugin, declarado em `.claude/settings.json` (marketplace + enabledPlugins) e instalado ao confiar no projeto.

### 4.3 Skills opcionais (instalação manual, se necessário)

Avaliadas e consideradas cobertas, redundantes ou prematuras hoje; instalar apenas se surgir necessidade real:

- `gh-issues-auto-fixer` (mcpmarket) — ciclo automático de issues→fix→PR; o essencial é coberto por `github-ops`.
- `spec-driven-development` (mcpmarket) — este template já implementa SDD (task→refinamento→PRD→gates).
- `mp-pdf-data-extractor` (mcpmarket) — o Claude lê PDFs nativamente (tool Read).
- `mp-sql-copilot` (mcpmarket) — parcialmente coberta por `postgres-patterns`.
- ~~**Observabilidade de aplicação**~~ — resolvida em 2026-07-07: `golang-observability-opentelemetry` (bobmatnyc/claude-mpm-skills) vendorizada após a definição da stack (ver §4.2).
- **Levantamento de 2026-07-07** (stack definida; avaliadas e **não** adotadas): `rabbitmq-expert` (awesomeskill.ai) — centrada em Python/pika, má aderência a Go; RabbitMQ fica coberto por `docs/lib/CACHE-MESSAGING.md` + Context7. `stripe-mcp-skill` (terceiro, pouco mantido) — quando chegar o E6, preferir o **MCP oficial da Stripe** (candidata registrada). Agentes externos (ex.: `golang-pro` do VoltAgent) — rejeitados: colidiriam com o `backend-dev` (executor único); conhecimento Go entra como skill de apoio (§4.2). As demais ~42 skills do samber/cc-skills-golang ficam como pool opcional — vendorizar só com necessidade real (ex.: `golang-performance` no E12, `golang-continuous-integration` no E0c).
- `canary-watch` / `production-audit` (ECC) — verificação pós-deploy e prontidão de produção; vendorizar quando houver app deployada.
- `codebase-onboarding` (ECC) — mapa de codebase existente; útil ao aplicar este template em repositório legado.
- `hookify-rules` / `delivery-gate` (ECC) — candidatas para o enforcement técnico do gate de auditoria (§6.4.6), quando formos implementá-lo.

### 4.4 Uso econômico de skills (regras de consumo)

1. **Carga seletiva:** cada agente carrega no máximo as **1–2 skills diretamente pertinentes** à tarefa — indicadas no brief do refinamento (§6.14.3) e no PRD. Nunca varrer o catálogo "por garantia": cada skill invocada entra inteira no contexto do agente.
2. **Estacionamento por fase:** skills de fase futura vivem em **`.claude/skills-parked/`** — fora do catálogo, não custam nada em nenhum prompt. Reativar = mover de volta para `.claude/skills/` na task que precisar (mudança trivial, sem cerimônia). Estado atual:

| Skill estacionada | Reativar em |
|---|---|
| ~~`frontend-patterns`, `design-system`~~ | **reativadas em 2026-09-29** (E5) |
| ~~`e2e-testing`~~ · `browser-qa` | `e2e-testing` **reativada em 2026-09-29** (task 0021, Playwright do M3); `browser-qa` segue estacionada (E8+) |
| `benchmark` | E12 (teste de carga) |
| ~~plugin `frontend-design`~~ | **reabilitado em 2026-09-29** (E5) |

> O plugin `ui-ux-pro-max` permanece **ativo** (decisão do usuário, 2026-07-08) — não estacionar.

3. As listas do §4.2 permanecem a referência completa — skill estacionada continua listada, com o estado registrado aqui.

---

## 5. Arquivos de controle

| Arquivo | Conteúdo | Quem atualiza | Quando |
|---|---|---|---|
| `roles.md` | política completa (este arquivo) | sessão principal, com aval do usuário | a cada regra estrutural nova |
| `doc.md` | mini-UML da aplicação: visão, atores, componentes, domínio, fluxos críticos, RNFs, volumetria, trade-offs | sessão principal, com aval do usuário | criado na descoberta (§6.15); atualizado quando arquitetura/domínio/fluxo crítico mudar |
| `lib.md` | registro de dependências e versões | quem adiciona dependência | **toda** nova dependência (§6.9) |
| `state.md` | estado atual + histórico recente | sessão principal | fim de cada task e antes do PR |
| `plan.md` | plano vivo da task em andamento | sessão principal durante implementação | 3 checkpoints: abertura, meio/desvio, fechamento (§6.11) |
| `docs/roadmap.md` | visão geral das entregas | sessão principal, com aval do usuário | quando prioridades mudarem |

---

## 6. Regras

### 6.1 ADR (Architecture Decision Records)
1. Toda decisão arquitetural **relevante** deve ser registrada em ADR (`docs/adr/`).
2. ADR só é criado com **necessidade real E autorização explícita do usuário**. Agentes recomendam; nunca criam por conta própria.
3. Não criar ADR para decisões triviais.
4. Todo ADR deve deixar claro: motivo, o que foi considerado e descartado, trade-offs e estratégias para minimizá-los.
5. ADRs devem ter escopo claro e listar ADRs relacionados.
6. Critérios de relevância: afeta estrutura, tecnologia, modelo de dados ou contratos entre módulos/sistemas.

### 6.2 PRD (Product Requirements Document)
1. **Toda task deve ter um PRD antes da implementação** (`docs/prd/`).
2. O PRD orienta a implementação e delimita o escopo — nada fora dele sem atualizá-lo primeiro.
3. O PRD indica claramente os arquivos previstos para criação/alteração.
4. Se a implementação divergir do plano, o PRD deve ser atualizado.
5. O PRD é gerado **just-in-time**, ao abrir a task, pela sessão principal — consumindo as exigências do refinamento do épico (§6.14), **sem nova rodada de agentes**. Não gerar PRDs de tasks futuras antecipadamente: eles envelheceriam antes de servir.

### 6.3 Task
1. Escopo pequeno, revisável com clareza.
2. **Máximo de 30 arquivos alterados**; acima disso, quebrar em tasks menores.
3. Cada task tem: branch própria, PRD próprio, critérios de aceite claros.
4. Rastreável em `state.md` e `plan.md`.

### 6.4 Auditoria (gate obrigatório — modelo híbrido)
1. **Nenhum merge sem o gate completo: CI verde + revisão de julgamento aprovada.** Pré-push, o requisito é **scan de secrets limpo** (gitleaks local; push protection do GitHub como segunda barreira).
2. **Itens mecânicos — automatizados no CI** (`.github/workflows/ci.yml`, desde a task 0004): lint (`golangci-lint`), `go vet`, secrets (gitleaks — working tree + histórico), CVEs (`govulncheck`), `sqlc vet` + diff do código gerado, testes (`go test -race`, suíte completa com testcontainers), migrations validadas em PG efêmero (job condicional por path) e build `linux/arm64` + smoke da imagem. CI roda no GitHub Actions — custo zero de tokens.
3. **Itens de julgamento — um único passe de revisão sobre o diff do PR** (um agente revisor, não dois): aderência ao PRD, conformidade com ADRs, overengineering, PII em logs, qualidade geral.
4. **Reauditoria escopada:** veredito REPROVADO → corrigir e revalidar **apenas os itens reprovados** (máx. 1 rodada extra; persistindo, escalar ao usuário). Nunca re-auditar o diff inteiro por correção pontual.
5. **Itens mecânicos ainda sem job próprio** (cobertos no passe de julgamento até serem automatizados): gate de cobertura mínima (aguarda baseline — fora de escopo do PRD 0004), contagem ≤ 30 arquivos, diff `go.mod` × `lib.md`, presença/atualização de `plan.md` e `state.md`.
6. Branch protection da `main` exige o check `ci` (task 0004). Evolução futura: hook PreToolUse bloqueando `git push` com secrets/gate pendente.

### 6.5 Git e Pull Request
1. Branches: `feature/NNNN-nome` · `fix/NNNN-nome` · `chore/NNNN-nome` · `refactor/NNNN-nome` (NNNN = id da task).
2. Nunca misturar múltiplas tasks na mesma branch.
3. Sem push sem o gate pré-push (§6.4.1); sem PR sem PRD e testes mínimos; sem merge sem CI verde + revisão de julgamento aprovada.
4. PR segue `.github/pull_request_template.md`: resumo, task, PRD, ADRs, arquivos principais, testes executados, riscos, evidências.
5. O CI/CD executa todos os testes após a abertura do PR.

### 6.6 Segurança
1. Secrets nunca em código, commit ou log — usar variáveis de ambiente/secret manager.
2. Logs não podem conter dados sensíveis (PII, tokens, credenciais).
3. Toda dependência nova passa por verificação de CVEs antes de entrar (registrar em `lib.md`).
4. Autenticação, autorização e controle de acesso validados pelo agente `security`.
5. Riscos de segurança avaliados **antes** do push e da abertura do PR.

### 6.7 Testes
1. QA define a cobertura mínima; prioridade para o MVC da aplicação e rotinas críticas.
2. Testes devem ser **idempotentes**, confiáveis e sem dependência de estado compartilhado indevido.
3. Cobrir cenários de erro, borda e fluxos principais — validar comportamento de negócio, não implementação.
4. Suíte simples de executar localmente e no pipeline.

### 6.8 Observabilidade
1. O sistema nasce preparado para observabilidade: logs estruturados, métricas e traces conforme padrões do agente `sre-devops`.
2. Cada dependência da aplicação deve ser monitorável.
3. Rotinas críticas (queries, jobs, integrações, processamento assíncrono) devem expor sinais que permitam identificar gargalos.

### 6.9 Dependências
1. **Toda nova dependência deve ser registrada no `lib.md`** com justificativa — nenhuma entra "de carona".
2. Avaliar: necessidade real, estabilidade, manutenção ativa, CVEs, alternativas.
3. Preferir a biblioteca padrão / recursos nativos quando resolverem o problema.
4. A documentação técnica de consulta das dependências vive em **`docs/lib/`** (uma página por grupo, índice no `README.md`), elaborada com apoio do Context7 (§6.12). Ela **complementa** o `lib.md` (registro/justificativa/versões) — não o substitui; em divergência, prevalecem o `lib.md` e a consulta atual ao Context7. Atualizar a página correspondente quando uma dependência entrar no build ou mudar de versão.

### 6.10 Migrations
1. Toda alteração estrutural de banco tem migration versionada — nunca alterar schema manualmente.
2. Migration vinculada à task e ao PRD; rollback definido quando aplicável; idempotente quando possível.
3. Validada localmente (docker-compose) ou nos testes antes do push.
4. **Boas práticas de SQL são critério de aprovação** (detalhes em `.claude/skills/criar-migration/boas-praticas-sql.md`): toda FK nova indexada; índices justificados pelos padrões de acesso do PRD; queries de fluxo crítico validadas com `EXPLAIN` — **sem full scan não justificado em tabela grande**; operações compatíveis com zero-downtime (índice concorrente, constraint `NOT VALID`+`VALIDATE`, backfill em lotes, expand-contract) quando houver ambiente com tráfego.

### 6.11 Arquivos de controle
1. `plan.md` atualizado em **3 checkpoints** — abertura da task, meio (ou a qualquer desvio de rota relevante) e fechamento — **não a cada commit**. O registro contínuo fino fica com a memória de sessão (§6.13); o plan.md reflete o estado real da task em cada checkpoint.
2. `state.md` atualizado ao **final** de cada task e antes do PR — **consolidado num único commit de fechamento** junto com o registro de auditoria e o checkpoint final do plan.md (não espalhar em commits `docs:` avulsos).
3. `roles.md` atualizado a cada regra estrutural nova.
4. *(Regra de economia — decisão do usuário, 2026-07-12: ~40% dos commits até a task 0004 eram `docs:` de controle; o batching corta ~15–40k tokens/task sem perder rastreabilidade.)*

### 6.12 MCP Context7
1. Sempre que houver dúvida sobre biblioteca, framework ou versão, **consultar o Context7** (configurado em `.mcp.json`).
2. Não assumir comportamento de dependência sem validação.
3. Usar para apoiar ADRs, PRDs, planos de implementação e análise de dependências.

### 6.13 Segundo cérebro (vault Obsidian)
1. O vault Obsidian (caminho em `OBSIDIAN_VAULT_PATH` no `~/.claude/settings.json`) é o **segundo cérebro** do ciclo de desenvolvimento: conhecimento destilado das sessões (decisões, aprendizados, contexto de projeto) vive lá, seguindo as regras AI-first do `_CLAUDE.md` do vault.
2. **O hook `SessionEnd` é o registro padrão da sessão no vault** (automático, sem custo na sessão principal) — não duplicar com cerimônia manual. Ao final de task **com decisão relevante**: `/obsidian-decide` (apenas decisões). `/obsidian-log` fica para exceções (hook inativo ou sessão que mereça registro imediato).
3. Conversas que produzirem insight além da task: salvar com `/obsidian-save`.
4. A nota do projeto no vault (`Projects/<nome>.md`) deve refletir o estado real — atualizar `Recent Activity` e `Key Decisions` quando houver mudança relevante.
5. **Hooks de sessão do repositório** (`.claude/settings.json` → `.claude/hooks/`): `SessionEnd` grava automaticamente o resumo da sessão no vault (Dev Log + propagação) e `SessionStart` injeta o contexto do projeto ao abrir o chat. Exigem `OBSIDIAN_VAULT_PATH` no ambiente da máquina — sem ele ficam inertes, sem erro. Kill switch: `OBSIDIAN_SESSION_SUMMARY_ENABLED=0`. O hook PostCompact (nível de usuário, opt-in via `OBSIDIAN_BG_AGENT_ENABLED`) é complementar. Os comandos acima cobrem o registro deliberado.

### 6.14 Refinamento multiagente (cerimônia por épico)
1. **O refinamento acontece uma vez por épico**, antes da primeira task dele (skill `refinar-task`): os 5 agentes analisam o épico inteiro e produzem **exigências por task planejada** — nenhum PRD do épico sem esse refinamento.
2. Os **5 agentes participam**, cada um com parecer do seu domínio: arquiteto (arquitetura/trade-offs), sre-devops (infra/observabilidade), security (riscos), qa (testes/aceite), backend-dev (viabilidade/esforço).
3. **Dieta de contexto:** os agentes recebem um **brief pré-digerido** montado pela sessão principal (épico + decisões relevantes do `doc.md` + trechos dos ADRs que tocam o tema) — não releem o repositório inteiro. Pareceres consultivos podem rodar em **modelo menor** (override no Agent tool); a implementação permanece no modelo principal.
4. Os pareceres são **confrontados em debate**: toda divergência é explicitada e resolvida citando as regras deste arquivo, ou — quando arquiteturalmente relevante e sem consenso — **escalada ao usuário**. Nunca decidir sozinho o que os agentes não convergirem.
5. A conclusão é **registrada em `docs/refinamentos/ENN-nome.md`** (pareceres, debate, conclusão, exigências por task) — cada PRD do épico DEVE incorporar as exigências da sua task.
6. **Camadas de cerimônia:** task **trivial** (docs, chore, fix pequeno, sem decisão de projeto) dispensa refinamento; task **padrão** consome o refinamento do épico sem nova rodada; épico **XL** (ex.: E6 — saga) pode ter rodada complementar focada numa task crítica, a critério do usuário.
7. Perguntas escaladas ao usuário **bloqueiam os PRDs afetados** até resposta.
8. O refinamento abre o ciclo de qualidade; o gate de auditoria (§6.4) o fecha — um não substitui o outro.

### 6.15 Descoberta de projeto (cerimônia de abertura)

1. **Todo projeto novo começa pela skill `iniciar-projeto`**, antes do roadmap — nenhum roadmap sem descoberta.
2. A entrevista cobre no mínimo: nome do projeto, escopo, requisitos funcionais e não funcionais, volumetria, orçamento, atores, público-alvo, dependências/integrações, arquitetura, stack, infra, **observabilidade/resiliência** (etapa SRE — ferramentas, métricas/SLOs, logs, alertas, padrões saga/compensação/retry/DLQ/rollback; bloco I do banco de perguntas) e **design/identidade visual da aplicação** (bloco J — direção estética, referências, tom, acessibilidade). Máximo de perguntas: profundidade é obrigação, não cortesia.
3. **As escolhas do usuário são confrontadas**: os 5 agentes analisam o dossiê e **todos os trade-offs relevantes são apresentados num quadro consolidado** antes das decisões — o usuário escolhe o caminho de cada divergência e **define as prioridades** do roadmap. A palavra final é do usuário; escolhas que contrariem recomendação são registradas no `doc.md` com o trade-off aceito.
4. A descoberta produz obrigatoriamente: `doc.md` (mini-UML da aplicação), `lib.md` inicial, `docs/roadmap.md` inicial (incluindo a metodologia) e a **direção de design inicial** em `docs/design/`.
5. Stack e dependências candidatas são validadas no Context7 (§6.12) antes de entrar no `lib.md`, marcadas como `planejada` até entrarem no build.
6. **ADRs iniciais:** ao final da descoberta, as decisões estruturais tomadas no confronto são propostas como ADRs e criadas **em bloco, mediante autorização explícita do usuário** (§6.1 permanece: sem autorização, ficam listadas como candidatas no `doc.md`).
7. **Levantamento de skills:** definida a stack na entrevista, buscar e avaliar skills/agentes de apoio para ela (processo do §4.3) — propor ao usuário e vendorizar as aprovadas, registrando origens no `lib.md`.
8. Ao final: `state.md` atualizado e sessão registrada no vault (§6.13).

---

## 7. Estrutura de documentação

```
docs/
├── roadmap.md            visão geral das entregas
├── playbook-backend.md   padrões de sistemas distribuídos contextualizados ao projeto (consulta seletiva do backend-dev)
├── playbook-database.md  problemas comuns de PostgreSQL/SQL: identificar e resolver (consulta seletiva do backend-dev)
├── playbook-security.md  superfícies de risco, identificação e correção (consulta seletiva do security e do backend-dev)
├── design/               identidade visual e protótipos (referência p/ PRDs de UI)
├── lib/                  docs técnicos das dependências do lib.md (consulta; base Context7)
├── refinamentos/ENN-nome.md   refinamento por épico: pareceres, debate, exigências por task (§6.14)
├── adr/NNNN-titulo-kebab.md
├── prd/NNNN-titulo-kebab.md
└── tasks/NNNN-titulo-kebab.md
```

Numeração sequencial de 4 dígitos por diretório, começando em `0001`. Cada diretório tem um `README.md` com índice. Templates ficam nas pastas das skills correspondentes (`.claude/skills/criar-*/`).

---

## 8. Manutenção deste arquivo

- Este arquivo é a **única** fonte de regras. Não duplicar regras em agentes, skills ou CLAUDE.md — referenciar por seção (ex.: "ver roles.md §6.4").
- Alterações estruturais na governança exigem atualização imediata deste arquivo e aval do usuário.

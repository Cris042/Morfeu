# Identidade Visual — "A Sala Escura"

> Exploração de design conduzida e **aprovada pelo usuário em 2026-07-07**.
> Protótipo navegável: [`prototipo-v1.html`](prototipo-v1.html) (autocontido, fontes embutidas — abrir direto no navegador).
> Este documento **não é ADR** (não é decisão arquitetural): é a referência de design que os PRDs dos épicos de UI (E0c, E5, E8) devem incorporar. Divergências durante a implementação atualizam este documento.

## 1. Conceito

A interface é a **sala de cinema no instante em que as luzes se apagam** — ancorada no nome do produto (Morfeu, deus dos sonhos) e no mundo físico do cinema. Três derivações:

1. O fundo é **noite azul-violeta**, nunca preto puro (`Noite`).
2. A luz que guia o olhar é o **tungstênio do projetor** (`Tungstênio`) — foco, hover, realce.
3. A única cor de ação é a **papoula**, a flor de Morfeu (`Papoula`) — só ela chama atenção.

**Decisão deliberada:** o produto do cliente vive num único mundo escuro, como a sala. Um eventual tema claro fica reservado ao backoffice do operador (decidir no E9). Uma variação clara ("saguão iluminado" no login) foi explorada e **descartada pelo usuário** — preservada no histórico de versões do artifact.

**Assinatura da marca:** o feixe do projetor com poeira em suspensão (canvas animado) na tela de entrada, mais a sequência de carga "luzes que se apagam". Todo o resto fica quieto e disciplinado.

## 2. Paleta

Tokens CSS (nomes em PT, seguindo o idioma do domínio — ADR 0004):

| Token | Hex | Papel |
|---|---|---|
| `--noite` | `#14101F` | fundo — a sala às escuras |
| `--sala` | `#1E1830` | superfícies e cartões |
| `--veludo` | `#2A2145` | bordas e elevação (o veludo das poltronas) |
| `--tela` | `#F2ECDF` | texto principal — a luz da tela (marfim quente) |
| `--nevoa` | `#A79DC4` | texto secundário |
| `--tungstenio` | `#E9B95B` | foco, hover, realce — a luz do projetor |
| `--papoula` | `#FF5C45` | ação primária (texto sobre ela: `#2A100B`) |

**Contraste (WCAG), calculado em 2026-07-07:**

| Par | Razão | Nível |
|---|---|---|
| Tela sobre Noite (texto principal) | 15,88:1 | AAA |
| Névoa sobre Noite (texto secundário) | 7,35:1 | AAA |
| Névoa sobre Sala (cartões) | 6,73:1 | AA (AAA p/ texto grande) |
| Tungstênio sobre Noite (eyebrow/realce) | 10,28:1 | AAA |
| Tinta sobre Papoula (botão primário) | 5,83:1 | AA |
| Tela sobre Veludo (aba ativa) | 12,72:1 | AAA |
| Escuro sobre Tungstênio (chip selecionado) | 9,31:1 | AAA |

**Cores semânticas fixas:** a classificação indicativa usa as cores oficiais do **ClassInd** (L verde `#00A54F`, 12 amarelo `#F8C300`, 14 laranja `#E67824`, 16 vermelho `#DB2827`, 18 preto) — vocabulário que o público brasileiro já conhece. Não contam como cor de ação.

## 3. Tipografia

| Papel | Família | Uso |
|---|---|---|
| Display | **Fraunces** (600; 500 itálico p/ voz da marca) | wordmark, títulos de tela e de filme |
| Corpo/UI | **Schibsted Grotesk** (400/500/700) | texto corrido, formulários, botões |
| Utilitária | **Spline Sans Mono** (400/500) | horários, salas, fileira/assento, códigos de pedido, eyebrows |

Regras: horários e códigos **sempre** em mono com `font-variant-numeric: tabular-nums`; eyebrows em mono uppercase com `letter-spacing: .18em`; títulos com `text-wrap: balance`; texto corrido ≤ 65ch.

> **Implementado na task 0017:** fontes **self-hosted** como arquivos woff2 variáveis em `web/public/fonts/` (mesmos arquivos do protótipo — SHA-256 conferido contra `fonts.gstatic.com`), com a licença OFL; `@fontsource/*` descartado. Nunca Google Fonts via CDN (privacidade/LGPD + same-origin + CSP `font-src 'self'`). Tokens em `web/src/styles/tokens.css`.

## 4. Movimento

| Regra | Descrição |
|---|---|
| Luzes que se apagam | Ao abrir o app, a sala "escurece" por ~1,7s — o conteúdo surge como a tela acendendo. Uma vez por visita, nunca em navegação interna. |
| Poeira no feixe | O feixe do projetor com partículas em suspensão vive **apenas** na entrada (login); dentro do app o ambiente fica quieto. |
| Corte de cena | Trocas de tela passam pelo escuro em ~500ms, como corte de montagem — nunca slide, nunca zoom. |
| Brilho de tungstênio | Hover e foco acendem em tungstênio (borda + halo suave). A papoula não pisca nem pulsa: cor de ação é estável. |
| Respeito ao usuário | Com `prefers-reduced-motion`, tudo vira estado estático — o feixe permanece, a poeira para. Obrigatório em todo componente animado. |

## 5. Voz e microcopy

Fala como quem trabalha na bilheteria de um cinema de bairro: direto, caloroso, sem jargão de sistema.

- Botões dizem o que fazem: **"Escolher assentos"**, nunca "Continuar"/"Submit".
- Erros explicam o que houve e apontam a saída; nunca se desculpam nem são vagos.
- O convidado é tão bem-vindo quanto quem tem conta (fluxo único convidado+conta, decisão da descoberta) — o login sempre mostra a saída "comprar só com o e-mail".
- Frase da marca (Fraunces itálico): *"A sala já está escura. Falta você."*

## 6. Componentes prototipados

O protótipo v1 cobre: **login/cadastro** (abas, campos, nota de convidado, feixe do projetor) e **home/cartaz** (topbar com saudação pela hora, destaque com sessões selecionáveis em chips mono — incluindo estado "esgotada" —, grade de filmes com pôster, ClassInd, gêneros e horários). Detalhes de forma: raios pequenos (cards 8px, chips 3px — nada de `rounded-lg` genérico), grão de película a 5% sobre tudo, pôsteres tipográficos enquanto não há integração TMDB.

## 7. Rastreabilidade

- Origem: exploração de design fora do fluxo de task (sem código de produção), sessão de 2026-07-07.
- Artifact (histórico de versões): `v1-sala-escura` → `v2-saguao-iluminado` (descartada) → `v3-restaura-v1` (aprovada = v1).
- Consumidores: PRDs do E0c (shell da SPA), E5 (SPA cliente), E8 (checkout), E9 (backoffice — decidir tema).
- Relacionados: `doc.md` §7 (RNF a11y/i18n), ADR 0004 (idioma PT no domínio), ADR 0006 (Playwright usa `data-testid`, imune a mudanças de copy).

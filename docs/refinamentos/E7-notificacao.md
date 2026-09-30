# Refinamento — Épico E7 (Notificação: e-mail + QR) — 2026-09-30

Cerimônia por épico (roles.md §6.14): brief único → pareceres (`arquiteto`, `security`, `qa`, `sre-devops`, depois `backend-dev`) → debate (com verificação no Context7) → 3 perguntas escaladas e **respondidas pelo usuário no mesmo dia**. Sem ADR novo: os ADRs 0005 (Strategy `EmailSender`) e 0010 (token HMAC) cobrem as decisões estruturais.

## Pareceres

| Agente | Posição | Pontos-chave |
|---|---|---|
| arquiteto | 2 tasks, sem ADR | `notificacao.FonteDoEmail` satisfeita por adapter no main compondo portas mínimas de pedido+sessao+catalogo; token calculado no `pedido` (a notificação nunca vê o segredo); um adapter real (Resend, HTTP cru) + fake; QR PNG inline por CID; página pública no E8; estorno por evento `pedido.estornado` |
| security | 2 tasks, sem ADR | token `HMAC(seg_v, "ingresso:v1:"+id)` 256 bits; **o HMAC não resolve de volta ao ingresso** → link `/i/<ingresso_id>.<token>`; expiração e revogação no servidor (E8); sem e-mail/token em logs; `html/template`; assunto sem dado livre; API key só env com escopo de envio; fake recusado em produção; nunca enviar QR de pedido não pago/ingresso não ativo |
| qa | 2 tasks | pirâmide sem rede (fake gravador + `httptest` do provedor); **QR decodificado no teste**; golden do template + escape + sem pixel; venda intacta com provedor sempre falhando; Idempotency-Key idêntica na redelivery; regressões do E6 |
| sre-devops | 2 tasks | preferia Brevo (entrega sem domínio); HTTP cru, timeout 5 s, retry só pelo consumidor; 4xx → DLQ, 5xx/429/timeout transitório; métricas `morfeu_email_envios_total{provedor,tipo,resultado}` + duração; 2 alertas; Mailpit opcional |
| backend-dev | 3 tasks | núcleo (portas + token + QR + template + fake) → provedor (Resend + config + métricas + alertas) → estorno; `ErrNaoNotificavel` como skip; `pedido.estornado` na TX do estorno; `sessao`/`catalogo` precisam de métodos sem filtro de "aberta/arquivado"; `skip2/go-qrcode` + `gozxing` só em teste |

## Debate (divergências e resolução)

1. **Resend × Brevo** (arquiteto/backend × SRE). Verificado no Context7: Resend tem anexo inline por `content_id` e `Idempotency-Key` (24 h); Brevo não tem CID nem idempotência na API HTTP. → **Escalado** (junto com o domínio): usuário escolheu **Resend** com demo limitada ao dono da conta até haver domínio.
2. **Formato do link** — `/i/{token}` × `/i/{id}.{token}`. → **Consenso**: `/i/{ingresso_id}.{token}` (HMAC não é reversível; o servidor recomputa e compara). A página pública e a expiração pós-sessão são do **E8**; o E7 fixa o formato e a `BASE_URL_PUBLICA`.
3. **Mailpit + SMTP × só fake** (SRE × arquiteto/backend). → **Consenso**: só fake (anti-overengineering; o golden do template cobre o conteúdo). O fake pode gravar o HTML em arquivo local no dev.
4. **Onde vive o token** → **Consenso**: `pedido` (dono dos ingressos), entregue já calculado pela porta.
5. **Retry** → **Consenso**: só pela redelivery do consumidor (`x-delivery-limit=3`), sem retry interno; cota diária (429) → DLQ + métrica, replay no dia seguinte.
6. **Pedido não notificável** (não pago, sem ingresso ativo, estornado) → **Consenso**: skip com ack + métrica `resultado=ignorado` — nunca envia QR de ingresso inválido.
7. **E-mail de estorno no E7** → **Escalado**: usuário aceitou **3 tasks** (estorno na T3).
8. **Lib do QR** → **Escalado**: usuário escolheu **`skip2/go-qrcode`** (zero dependências; sem release desde 2020 — justificativa no `lib.md`). `makiuchi-d/gozxing` só em teste (decodificar o PNG). OSV sem advisories para as duas (2026-09-30).

## Conclusão

- 3 tasks (0028–0030). Nenhum ADR. Dependências novas: `skip2/go-qrcode` (runtime), `makiuchi-d/gozxing` (teste) — Context7/OSV/`lib.md` na T1. Provedor por HTTP cru (sem SDK).
- **M4** passa a ser validado com o gateway fake + e-mail fake no CI e com envio real só para o e-mail do dono da conta Resend; entrega a terceiros depende de domínio verificado (pendência).
- Riscos: (1) e-mail pendurado na TX do dedup (até 5 s) — aceito no volume, registrado no PRD; (2) duplicata se o provedor aceitou e o commit do dedup falhou — coberta pela `Idempotency-Key` (24 h); (3) link do e-mail aponta para página que só existe no E8 — o QR e o código do pedido já servem sem a página.

## Exigências por task

### T1 (0028) — Núcleo: portas, token, QR, template e consumidor real (fake)

- `pedido.TokenIngresso(segredo, versão, ingresso_id)` = HMAC-SHA256(segredo_vN, `"ingresso:v1:"+id`) em base64url (43 chars), comparação futura com `hmac.Equal`; segredos por versão só por env (≥ 32 bytes; boot recusa ausente/curto/padrão em produção); chave exclusiva desta finalidade.
- Porta `pedido.DadosParaNotificacao(id)` → status, e-mail, código, sessão e ingressos **ativos** com token calculado; `ErrNaoNotificavel` se não `pago` ou sem ingresso ativo. `sessao.DadosParaIngresso(id)` e `catalogo.TituloDoFilme(id)` sem filtro de sessão aberta/filme arquivado. Adapter `FonteDoEmail` no main.
- `notificacao`: interface `EmailSender` (Strategy — ADR 0005) + fake gravador (latência/falha injetáveis) recusado em produção; `Entregar` real = carregar → montar → enviar; `ErrNaoNotificavel` = ack + `resultado=ignorado`; pedido inexistente = permanente.
- Template `html/template` + texto puro, sem `template.HTML`, sem pixel/tracking, assunto fixo sem dado livre, datas em `America/Sao_Paulo`, URL só da `BASE_URL_PUBLICA`; um QR PNG por ingresso (payload `https://{base}/i/{id}.{token}`) inline por CID.
- Testes: token (determinístico, 43 chars, muda com a versão/segredo, sem id em claro); **QR decodificado** (gozxing) = URL esperada, N assentos → N QRs; golden do HTML/texto (relógio injetado) + escape de título hostil + sem `<img>` externa; não-notificável não envia; PII fora dos logs (captura); regressões do E6 (latência só após envio ok, DLQ, panic).
- `lib.md` com as duas libs (justificativa do skip2).

### T2 (0029) — Provedor Resend, config, métricas e alertas

- Adapter Resend por HTTP cru: `POST /emails`, `Authorization: Bearer`, `Idempotency-Key` = `{tipo}-{pedido_id}`, anexos inline `content_id`, timeout 5 s, corpo de resposta limitado; sem retry interno.
- Mapa: 2xx ok; 429 de cota diária → permanente + `resultado=cota`; 429 de taxa, 5xx, timeout → transitório; demais 4xx → permanente; corpo de erro nunca logado sem sanitizar.
- Config: `EMAIL_PROVEDOR=fake|resend`, `RESEND_API_KEY`, `EMAIL_REMETENTE`, `BASE_URL_PUBLICA`, `INGRESSO_TOKEN_SEGREDO_V1`; boot recusa fake/sem chave/segredo fraco em produção; gitleaks `re_`.
- Métricas `morfeu_email_envios_total{provedor,tipo,resultado}` + `morfeu_email_envio_duracao_segundos{provedor}`; labels na allowlist da telemetria (lição da 0026); alertas: permanente/cota > 0 em 15 min; falha > 20% com ≥ 5 envios.
- Testes com `httptest` (200/422/429/500/timeout/corpo malformado; confere headers, Authorization, Idempotency-Key, CID) e de config.

### T3 (0030) — E-mail de estorno

- `pedido.estornado` (só `pedido_id`) enfileirado na **mesma TX** do `estornado` (tarefas.go); fila `notificacao.pedido_estornado` + DLX/DLQ próprias; DLQ na lista do `replay-dlq` e no gauge.
- Consumidor + template de texto curto **sem token nem QR**; `tipo=estorno` nas métricas.
- Testes: estorno concluído → 1 e-mail de estorno; pago-tarde-estornado recebe só o de estorno (nunca o ingresso); `estorno_pendente` não notifica; replay da nova DLQ.

### Exigências transferidas

- **E8**: página pública `/i/{id}.{token}` (resposta idêntica para inválido/inexistente, rate limit, expiração = início da sessão + 24 h → 410, revogação por status, `Referrer-Policy: no-referrer`, `no-store`, path redigido no access log, sem scripts de terceiros).
- **Deploy/E11**: domínio verificado (SPF/DKIM/DMARC, subdomínio de envio) para entregar a terceiros; registrar o Resend como suboperador LGPD.

### Perguntas escaladas ao usuário (respondidas em 2026-09-30)

1. Provedor/domínio → **Resend; demo só para o dono até haver domínio**.
2. Estorno no E7 → **sim, 3 tasks**.
3. Lib do QR → **skip2/go-qrcode**.

-- Invariante de assento do teste de carga (PRD 0045 RF08; ADR 0006/0010).
-- Fonte única: o teardown do E2E e o smoke/runbook de carga usam esta query.
-- ZERO linhas = íntegro. Cada linha é uma violação:
--   (a) assento com mais de um hold ativo|convertido (a trava do ADR 0008
--       garante 1 por (sessão, assento) — holds_assento_ocupado);
--   (b) assento com mais de um ingresso ativo (2ª linha de defesa —
--       ingressos_assento_ativo);
--   (c) pedido pago sem nenhum ingresso (o pivô emite ingressos na mesma TX).
SELECT 'assento_com_mais_de_um_hold_vivo' AS violacao,
       sessao_id::text                    AS referencia,
       assento_codigo                     AS assento,
       count(*)                           AS quantidade
FROM holds
WHERE status IN ('ativo', 'convertido')
GROUP BY sessao_id, assento_codigo
HAVING count(*) > 1
UNION ALL
SELECT 'assento_com_mais_de_um_ingresso_ativo',
       sessao_id::text,
       assento_codigo,
       count(*)
FROM ingressos
WHERE status = 'ativo'
GROUP BY sessao_id, assento_codigo
HAVING count(*) > 1
UNION ALL
SELECT 'pedido_pago_sem_ingresso',
       p.id::text,
       NULL,
       0
FROM pedidos p
WHERE p.status = 'pago'
  AND NOT EXISTS (SELECT 1 FROM ingressos i WHERE i.pedido_id = p.id)
ORDER BY 1, 2, 3;

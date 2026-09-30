-- Sessão de teste do E2E (PRD 0021 RF03): sala nova a cada execução (nome
-- com timestamp) e uma sessão agendada daqui a 2 dias para o filme 1 (seed
-- das migrations). O teste descobre a sessão pela API pública.
WITH sala AS (
    INSERT INTO salas (nome, layout)
    VALUES ('Sala E2E ' || to_char(clock_timestamp(), 'YYYYMMDDHH24MISSMS'),
            '{"fileiras":4,"colunas":8,"vaos":[{"fileira":"D","coluna":1}],"pcd":[{"fileira":"A","coluna":1}]}')
    RETURNING id
)
INSERT INTO sessoes (filme_id, sala_id, inicio, duracao_min, fim, preco_centavos)
SELECT 1, sala.id, date_trunc('minute', now()) + interval '2 days',
       100, date_trunc('minute', now()) + interval '2 days 2 hours', 3200
FROM sala;

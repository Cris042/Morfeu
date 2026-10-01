-- Invariantes do restore (task 0043, RF06). Executado por restore.sh depois do
-- pg_restore, com a tabela temporária `manifesto` (tipo, nome, valor) já
-- carregada. Qualquer violação aborta (ON_ERROR_STOP) com a lista completa.
DO $$
DECLARE
  falhas text[] := '{}';
  m record;
  contagem bigint;
  versao_manifesto text;
  versao_banco text;
  sujo boolean;
BEGIN
  -- 1. versão da migration igual à do manifesto e não suja
  SELECT valor INTO versao_manifesto FROM manifesto WHERE tipo = 'versao';
  SELECT version::text, dirty INTO versao_banco, sujo FROM schema_migrations;
  IF versao_banco IS DISTINCT FROM versao_manifesto THEN
    falhas := falhas || format('versão da migration %s difere do manifesto %s', versao_banco, versao_manifesto);
  END IF;
  IF sujo IS DISTINCT FROM false THEN
    falhas := falhas || 'schema_migrations está suja'::text;
  END IF;

  -- 2. mesma lista de tabelas
  FOR m IN
    (SELECT nome FROM manifesto WHERE tipo = 'tabela' EXCEPT SELECT tablename FROM pg_tables WHERE schemaname = 'public')
  LOOP
    falhas := falhas || format('tabela %s do manifesto ausente no restore', m.nome);
  END LOOP;
  FOR m IN
    (SELECT tablename AS nome FROM pg_tables WHERE schemaname = 'public' EXCEPT SELECT nome FROM manifesto WHERE tipo = 'tabela')
  LOOP
    falhas := falhas || format('tabela %s do restore não consta no manifesto', m.nome);
  END LOOP;

  -- 3. contagem por tabela igual ao manifesto
  FOR m IN SELECT nome, valor FROM manifesto WHERE tipo = 'tabela' AND EXISTS (SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = nome) LOOP
    EXECUTE format('SELECT count(*) FROM public.%I', m.nome) INTO contagem;
    IF contagem::text <> m.valor THEN
      falhas := falhas || format('tabela %s: %s linhas, manifesto %s', m.nome, contagem, m.valor);
    END IF;
  END LOOP;

  -- 4. nenhuma constraint NOT VALID
  FOR m IN SELECT conname FROM pg_constraint WHERE NOT convalidated LOOP
    falhas := falhas || format('constraint %s está NOT VALID', m.conname);
  END LOOP;

  -- 5. triggers da trilha de auditoria (barram UPDATE e TRUNCATE)
  FOR m IN SELECT t AS nome FROM unnest(ARRAY['eventos_auditoria_sem_update', 'eventos_auditoria_sem_truncate']) AS t
           WHERE NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = t AND NOT tgisinternal AND tgrelid = 'public.eventos_auditoria'::regclass) LOOP
    falhas := falhas || format('trigger %s ausente', m.nome);
  END LOOP;

  -- 6. índices únicos parciais da trava de assento (ADR 0008): holds (migration
  --    010 trocou holds_assento_ativo por holds_assento_ocupado) e ingressos
  FOR m IN SELECT x.tabela, x.indice FROM (VALUES ('holds', 'holds_assento_ocupado'), ('ingressos', 'ingressos_assento_ativo')) AS x (tabela, indice)
           WHERE NOT EXISTS (
             SELECT 1 FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
             WHERE c.relname = x.indice AND i.indisunique AND i.indpred IS NOT NULL AND i.indrelid = format('public.%I', x.tabela)::regclass) LOOP
    falhas := falhas || format('índice único parcial %s ausente', m.indice);
  END LOOP;

  IF cardinality(falhas) > 0 THEN
    RAISE EXCEPTION 'invariantes violadas: %', array_to_string(falhas, '; ');
  END IF;
  RAISE NOTICE 'invariantes ok (versão %, % tabelas)', versao_banco, (SELECT count(*) FROM manifesto WHERE tipo = 'tabela');
END
$$;

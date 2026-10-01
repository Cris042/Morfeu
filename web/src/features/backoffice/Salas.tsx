import { useState } from 'react'
import type { SubmitEvent } from 'react'

import { useSalas, useSalvarSala } from '../../api/backoffice'
import type { Sala } from '../../api/backoffice'
import { ErroApi } from '../../api/client'
import { Carregando, Falha, Vazio } from '../../ui/Estado'
import styles from './Backoffice.module.css'

/** Modelo de layout para a sala nova (template JSON — refinamento E3). */
export const LAYOUT_MODELO = JSON.stringify({ fileiras: 8, colunas: 12, vaos: [{ fileira: 'D', coluna: 6 }], pcd: [{ fileira: 'A', coluna: 1 }] }, null, 2)

function mensagemDe(erro: unknown): string {
  if (erro instanceof ErroApi) {
    switch (erro.codigo) {
      case 'nome_em_uso':
        return 'Já existe uma sala com esse nome.'
      case 'layout_em_uso':
        return 'A sala tem sessões programadas: o layout não pode mudar (só o nome).'
      case 'dados_invalidos':
        return 'Nome ou layout inválido: confira fileiras, colunas, vãos e assentos PCD.'
    }
  }
  return 'Não foi possível salvar a sala.'
}

/** Salas do operador (PRD 0038): lista e formulário com o layout em JSON. */
export function Salas() {
  const salas = useSalas()
  const [editando, setEditando] = useState<Sala>()
  return (
    <>
      <FormularioSala key={editando?.id ?? 'nova'} sala={editando} aoTerminar={() => { setEditando(undefined) }} />
      <h2 className={styles.subtitulo}>Salas</h2>
      {salas.isPending && <Carregando texto="Buscando as salas…" />}
      {salas.isError && <Falha texto="Não foi possível carregar as salas." aoTentar={() => void salas.refetch()} />}
      {salas.isSuccess && salas.data.length === 0 && <Vazio>Nenhuma sala cadastrada.</Vazio>}
      {salas.isSuccess && salas.data.length > 0 && (
        <ul className={styles.lista}>
          {salas.data.map((s) => (
            <li key={s.id} className={styles.item}>
              <span className={styles.nome}>{s.nome}</span>
              <span className={styles.meta}>
                {s.layout.fileiras} fileiras × {s.layout.colunas} colunas
              </span>
              <button type="button" className={styles.secundario} onClick={() => { setEditando(s) }}>
                Editar {s.nome}
              </button>
            </li>
          ))}
        </ul>
      )}
    </>
  )
}

function FormularioSala({ sala, aoTerminar }: { sala?: Sala; aoTerminar: () => void }) {
  const salvar = useSalvarSala()
  const [erro, setErro] = useState('')
  const [aviso, setAviso] = useState('')

  async function enviar(ev: SubmitEvent<HTMLFormElement>) {
    ev.preventDefault()
    const form = ev.currentTarget
    const dados = new FormData(form)
    const nome = dados.get('nome')
    const texto = dados.get('layout')
    setErro('')
    setAviso('')
    let layout: unknown
    try {
      layout = JSON.parse(typeof texto === 'string' ? texto : '')
    } catch {
      setErro('O layout não é um JSON válido.')
      return
    }
    try {
      const s = await salvar.mutateAsync({ id: sala?.id, nome: typeof nome === 'string' ? nome : '', layout })
      setAviso(sala ? `Sala "${s.nome}" atualizada.` : `Sala "${s.nome}" criada.`)
      if (!sala) {
        form.reset()
      }
      aoTerminar()
    } catch (e) {
      setErro(mensagemDe(e))
    }
  }

  return (
    <section aria-labelledby="titulo-sala">
      <h2 id="titulo-sala" className={styles.subtitulo}>
        {sala ? `Editar ${sala.nome}` : 'Nova sala'}
      </h2>
      <form className={styles.formulario} onSubmit={(ev) => void enviar(ev)}>
        <label className={styles.campo}>
          Nome
          <input name="nome" required maxLength={80} defaultValue={sala?.nome} />
        </label>
        <label className={styles.campo}>
          Layout (JSON)
          <textarea name="layout" required spellCheck={false} defaultValue={sala ? JSON.stringify(sala.layout, null, 2) : LAYOUT_MODELO} />
        </label>
        <p className={styles.erro} role="alert">
          {erro}
        </p>
        <p className={styles.aviso} role="status">
          {aviso}
        </p>
        <div className={styles.linha}>
          <button type="submit" className={styles.primario} disabled={salvar.isPending}>
            {sala ? 'Salvar sala' : 'Criar sala'}
          </button>
          {sala && (
            <button type="button" className={styles.secundario} onClick={aoTerminar}>
              Cancelar edição
            </button>
          )}
        </div>
      </form>
    </section>
  )
}

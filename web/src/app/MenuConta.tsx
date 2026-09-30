import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router'

import { sair, useSessao } from '../api/sessao'
import styles from './App.module.css'

/** Entrar / "Meus pedidos" + Sair no topo (PRD 0032). */
export function MenuConta() {
  const sessao = useSessao()
  const cliente = useQueryClient()
  const navegar = useNavigate()
  const [saindo, setSaindo] = useState(false)

  // Saiu (aqui ou em outra aba): o cache da conta não sobrevive.
  useEffect(() => {
    if (sessao.status === 'anonimo') {
      cliente.removeQueries({ queryKey: ['conta'] })
    }
  }, [sessao.status, cliente])

  if (sessao.status === 'carregando') {
    return null
  }
  if (sessao.status === 'anonimo') {
    return (
      <nav className={styles.menu} aria-label="Conta">
        <Link to="/consulta">Consultar pedido</Link>
        <Link to="/entrar">Entrar</Link>
      </nav>
    )
  }
  return (
    <nav className={styles.menu} aria-label="Conta">
      <span className={styles.usuario}>{sessao.usuario.nome}</span>
      <Link to="/conta/pedidos">Meus pedidos</Link>
      <button
        type="button"
        className={styles.sair}
        disabled={saindo}
        onClick={() => {
          setSaindo(true)
          sair()
            .catch(() => undefined) // a aba já esqueceu o token
            .finally(() => {
              setSaindo(false)
              void navegar('/')
            })
        }}
      >
        Sair
      </button>
    </nav>
  )
}

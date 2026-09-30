import type { ReactNode } from 'react'

import styles from './Estado.module.css'

export function Carregando({ texto = 'Acendendo o projetor…' }: { texto?: string }) {
  return (
    <p className={styles.estado} role="status">
      {texto}
    </p>
  )
}

export function Falha({ texto, aoTentar }: { texto: string; aoTentar: () => void }) {
  return (
    <div className={styles.estado} role="alert">
      <p>{texto}</p>
      <button type="button" className={styles.botao} onClick={aoTentar}>
        Tentar de novo
      </button>
    </div>
  )
}

export function Vazio({ children }: { children: ReactNode }) {
  return <p className={styles.estado}>{children}</p>
}

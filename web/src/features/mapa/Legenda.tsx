import styles from './MapaDeAssentos.module.css'

const ITENS = [
  { classe: styles.livre, marca: '', texto: 'Livre' },
  { classe: styles.selecionado, marca: '✓', texto: 'Selecionado' },
  { classe: styles.meu, marca: '●', texto: 'Seu (reservado)' },
  { classe: styles.ocupado, marca: '×', texto: 'Ocupado' },
  { classe: styles.pcd, marca: '♿', texto: 'PCD' },
]

export function Legenda() {
  return (
    <ul aria-label="Legenda do mapa" className={styles.legenda}>
      {ITENS.map((i) => (
        <li key={i.texto} className={styles.item}>
          <span aria-hidden="true" className={[styles.assento, styles.amostra, i.classe].filter(Boolean).join(' ')}>
            {i.marca}
          </span>
          {i.texto}
        </li>
      ))}
    </ul>
  )
}

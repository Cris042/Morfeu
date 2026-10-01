import { Navigate, NavLink, Route, Routes, useLocation } from 'react-router'

import { useSessao } from '../../api/sessao'
import { Carregando, Vazio } from '../../ui/Estado'
import styles from './Backoffice.module.css'
import { Filmes } from './Filmes'
import { Salas } from './Salas'

/**
 * Área do operador (PRD 0038), carregada em chunk próprio. O guarda de papel
 * é só experiência: quem decide é a API (RBAC em toda rota /backoffice/*).
 */
export function Backoffice() {
  const sessao = useSessao()
  const local = useLocation()
  if (sessao.status === 'carregando') {
    return <Carregando />
  }
  if (sessao.status === 'anonimo') {
    return <Navigate to={`/entrar?volta=${encodeURIComponent(local.pathname)}`} replace />
  }
  if (sessao.usuario.papel !== 'operador') {
    return (
      <section>
        <h1 className={styles.titulo}>Área restrita</h1>
        <Vazio>Esta área é só para a operação do cinema.</Vazio>
      </section>
    )
  }
  return (
    <section className={styles.backoffice}>
      <h1 className={styles.titulo}>Backoffice</h1>
      <nav className={styles.abas} aria-label="Backoffice">
        <NavLink to="/backoffice/filmes" className={({ isActive }) => (isActive ? styles.ativa : styles.aba)}>
          Filmes
        </NavLink>
        <NavLink to="/backoffice/salas" className={({ isActive }) => (isActive ? styles.ativa : styles.aba)}>
          Salas
        </NavLink>
      </nav>
      <Routes>
        <Route index element={<Navigate to="filmes" replace />} />
        <Route path="filmes" element={<Filmes />} />
        <Route path="salas" element={<Salas />} />
      </Routes>
    </section>
  )
}

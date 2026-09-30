import { Link, Route, Routes } from 'react-router'

import { Cartaz } from '../features/cartaz/Cartaz'
import { PaginaFilme } from '../features/filme/PaginaFilme'
import styles from './App.module.css'

// Shell da SPA: marca, rotas e a atribuição obrigatória do TMDB (refinamento E2).
export function App() {
  return (
    <div className={styles.pagina}>
      <header className={styles.topo}>
        <Link to="/" className={styles.marca}>
          Morfeu
        </Link>
      </header>
      <main>
        <Routes>
          <Route path="/" element={<Cartaz />} />
          <Route path="/filmes/:id" element={<PaginaFilme />} />
          <Route path="/sessoes/:id" element={<EscolhaEmBreve />} />
          <Route path="*" element={<NaoEncontrada />} />
        </Routes>
      </main>
      <footer className={styles.rodape}>
        <p>
          Dados e imagens de filmes fornecidos pelo TMDB. Este produto usa a API do TMDB, mas não é endossado nem
          certificado pelo TMDB.
        </p>
      </footer>
    </div>
  )
}

// Placeholder até a task 0020 (mapa integrado à trava).
function EscolhaEmBreve() {
  return (
    <section>
      <h1>Escolha de assentos</h1>
      <p>
        A escolha de assentos abre em breve. <Link to="/">Voltar ao cartaz</Link>
      </p>
    </section>
  )
}

function NaoEncontrada() {
  return (
    <section>
      <h1>Essa sala não existe</h1>
      <p>
        O endereço que você abriu não leva a nenhuma sessão. <Link to="/">Voltar ao cartaz</Link>
      </p>
    </section>
  )
}

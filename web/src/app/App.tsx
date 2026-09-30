import { Route, Routes } from 'react-router'

// Shell da SPA (PRD 0017 RF05): marca, rotas e a atribuição obrigatória do
// TMDB (refinamento E2). As telas reais chegam nas tasks 0018–0020.
export function App() {
  return (
    <>
      <header>
        <p className="mono">Morfeu</p>
      </header>
      <main>
        <Routes>
          <Route path="/" element={<h1>Em cartaz</h1>} />
          <Route path="*" element={<NaoEncontrada />} />
        </Routes>
      </main>
      <footer>
        <p>
          Dados e imagens de filmes fornecidos pelo TMDB. Este produto usa a API do TMDB, mas não é endossado nem
          certificado pelo TMDB.
        </p>
      </footer>
    </>
  )
}

function NaoEncontrada() {
  return (
    <section>
      <h1>Essa sala não existe</h1>
      <p>
        O endereço que você abriu não leva a nenhuma sessão. <a href="/">Voltar ao cartaz</a>
      </p>
    </section>
  )
}

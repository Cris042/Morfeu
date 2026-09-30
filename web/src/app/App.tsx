import { lazy, Suspense } from 'react'
import { Link, Route, Routes } from 'react-router'

import { Cartaz } from '../features/cartaz/Cartaz'
import { AcompanharPedido } from '../features/checkout/AcompanharPedido'
import { Checkout } from '../features/checkout/Checkout'
import { Entrar } from '../features/conta/Entrar'
import { MeusPedidos, PedidoDaConta } from '../features/conta/MeusPedidos'
import { PaginaFilme } from '../features/filme/PaginaFilme'
import { PaginaSessao } from '../features/sessao/PaginaSessao'
import styles from './App.module.css'
import { MenuConta } from './MenuConta'

// Demonstração do mapa só em dev (PRD 0019 RF06): em produção a constante é
// false e o import dinâmico some do bundle.
const DemoMapa = import.meta.env.DEV
  ? lazy(() => import('../features/mapa/DemoMapa').then((m) => ({ default: m.DemoMapa })))
  : undefined

// Shell da SPA: marca, rotas e a atribuição obrigatória do TMDB (refinamento E2).
export function App() {
  return (
    <div className={styles.pagina}>
      <header className={styles.topo}>
        <Link to="/" className={styles.marca}>
          Morfeu
        </Link>
        <MenuConta />
      </header>
      <main>
        <Routes>
          <Route path="/" element={<Cartaz />} />
          <Route path="/filmes/:id" element={<PaginaFilme />} />
          <Route path="/sessoes/:id" element={<PaginaSessao />} />
          <Route path="/sessoes/:id/pagamento" element={<Checkout />} />
          <Route path="/pedido/:id" element={<AcompanharPedido />} />
          <Route path="/entrar" element={<Entrar modo="entrar" />} />
          <Route path="/cadastro" element={<Entrar modo="cadastro" />} />
          <Route path="/conta/pedidos" element={<MeusPedidos />} />
          <Route path="/conta/pedidos/:id" element={<PedidoDaConta />} />
          {DemoMapa && (
            <Route
              path="/_demo/mapa"
              element={
                <Suspense fallback={null}>
                  <DemoMapa />
                </Suspense>
              }
            />
          )}
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

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router'

import { repetirSeTransitorio } from './api/consultas'
import { App } from './app/App'
import './styles/tokens.css'

const clienteQuery = new QueryClient({
  defaultOptions: { queries: { retry: repetirSeTransitorio, refetchOnWindowFocus: false } },
})

const raiz = document.getElementById('raiz')
if (!raiz) {
  throw new Error('elemento #raiz ausente no index.html')
}

createRoot(raiz).render(
  <StrictMode>
    <QueryClientProvider client={clienteQuery}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
)

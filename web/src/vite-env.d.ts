/// <reference types="vite/client" />

// Variáveis de build da SPA (PRD 0033). Nenhuma é segredo: a publishable key
// é pública por definição; o modo fake só existe em dev/CI.
interface ImportMetaEnv {
  readonly VITE_STRIPE_PK?: string
  readonly VITE_PAGAMENTO_MODO?: 'fake' | 'stripe'
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

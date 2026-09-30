import js from '@eslint/js'
import reactHooks from 'eslint-plugin-react-hooks'
import globals from 'globals'
import tseslint from 'typescript-eslint'

// Exigências de segurança do refinamento E5 (PRD 0017 RF07):
// - dangerouslySetInnerHTML proibido (equivale a react/no-danger; o
//   eslint-plugin-react não suporta ESLint 10);
// - nada de localStorage/sessionStorage (carrinho/sessão só no cookie HttpOnly);
// - fetch só em src/api/ (cliente único com credentials + anti-CSRF).
const proibicoes = {
  'no-restricted-syntax': [
    'error',
    {
      selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']",
      message: 'Proibido: renderize texto (XSS — refinamento E2/E5).',
    },
  ],
  'no-restricted-globals': [
    'error',
    { name: 'localStorage', message: 'Proibido: nada de estado de sessão no navegador.' },
    { name: 'sessionStorage', message: 'Proibido: nada de estado de sessão no navegador.' },
    { name: 'fetch', message: 'Use src/api/client.ts (credentials + X-Requested-With).' },
  ],
}

export default tseslint.config(
  { ignores: ['dist', 'node_modules'] },
  {
    files: ['**/*.{ts,tsx}'],
    extends: [js.configs.recommended, ...tseslint.configs.strictTypeChecked],
    languageOptions: {
      globals: globals.browser,
      parserOptions: { projectService: true, tsconfigRootDir: import.meta.dirname },
    },
    plugins: { 'react-hooks': reactHooks },
    rules: {
      ...reactHooks.configs.recommended.rules,
      ...proibicoes,
    },
  },
  {
    // O cliente HTTP é o único lugar autorizado a chamar fetch.
    files: ['src/api/**/*.ts'],
    rules: {
      'no-restricted-globals': [
        'error',
        { name: 'localStorage', message: 'Proibido: nada de estado de sessão no navegador.' },
        { name: 'sessionStorage', message: 'Proibido: nada de estado de sessão no navegador.' },
      ],
    },
  },
  {
    files: ['eslint.config.js'],
    extends: [js.configs.recommended],
  },
)

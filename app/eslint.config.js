import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    rules: {
      // Las reglas del React Compiler que react-hooks v7 activa en `recommended`
      // (purity, set-state-in-effect, refs, static-components, use-memo, immutability…)
      // NO aplican: este proyecto no compila con el React Compiler y el código
      // legacy usa los patrones clásicos (animaciones con setState en effect,
      // Math.random en render para decoración, refs-espejo de estado).
      // Refactorizarlas para silenciarlas sería riesgo de regresión sin ganancia.
      // REMOVE IF: se adopta el React Compiler. Ver
      // https://react.dev/learn/react-compiler/rules
      'react-hooks/purity': 'off',
      'react-hooks/set-state-in-effect': 'off',
      'react-hooks/set-state-in-render': 'off',
      'react-hooks/refs': 'off',
      'react-hooks/static-components': 'off',
      'react-hooks/use-memo': 'off',
      'react-hooks/immutability': 'off',
      'react-hooks/globals': 'off',
      'react-hooks/error-boundaries': 'off',
      'react-hooks/config': 'off',
      'react-hooks/gating': 'off',
      'react-hooks/unsupported-syntax': 'off',
      'react-hooks/incompatible-library': 'off',
      // Fast Refresh (regla de DX, no de corrección): los ficheros de shadcn/ui
      // y los contextos exportan variantes cva / hooks junto al componente,
      // patrón estándar que la regla no distingue.
      'react-refresh/only-export-components': 'off',
    },
  },
])

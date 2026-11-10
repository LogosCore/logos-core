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
  },
  {
    files: ['src/components/ui/**/*.{ts,tsx}'],
    rules: {
      'react-refresh/only-export-components': 'off',
    },
  },

  // Pre-existing debt from eslint-plugin-react-hooks v7, whose rules are the
  // React Compiler's own analysis and stricter than the v6 set this code was
  // written against. Eight errors across six files predated the lint gate in
  // .github/workflows/ci-frontend.yml; one is left.
  //
  // Same policy core/.golangci.yml states for the Go side: a linter that fires
  // on existing code gets the code fixed, or stays out with a note saying why.
  // This is the note. They are downgraded per file rather than globally, so the
  // rules stay at error everywhere else and a NEW violation still fails the
  // build — the list only shrinks.
  //
  // Fixed since: useIsMobile reads an external store (useSyncExternalStore);
  // sha1, search-input, hash-details-dialog and timeline's window reset set
  // state while rendering where it follows its inputs, or not at all; the
  // credential validity helpers are a module of their own
  // (credential-validity-values.ts). Left: timeline's pull of an older window
  // while the loaded ones are empty, an effect answering its queries' results
  // (windowing bookkeeping), so set-state-in-effect stays a warning there.
  {
    files: ['src/graphql/hooks/timeline.ts'],
    rules: {
      'react-hooks/set-state-in-effect': 'warn',
    },
  },
])

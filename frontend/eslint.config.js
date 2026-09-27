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
  // written against. Nine errors across seven files, all predating the lint gate
  // in .github/workflows/ci-frontend.yml.
  //
  // Same policy core/.golangci.yml states for the Go side: a linter that fires
  // on existing code gets the code fixed, or stays out with a note saying why.
  // This is the note. They are downgraded per file rather than globally, so the
  // rules stay at error everywhere else and a NEW violation still fails the
  // build — the list only shrinks.
  //
  // Each needs its own judgement, which is why none is done here:
  //
  //   - set-state-in-effect is a real cascading-render cost, and the fix differs
  //     every time: useIsMobile wants useSyncExternalStore, sha1 is an async
  //     result landing in state, timeline is windowing bookkeeping, search-input
  //     and hash-details-dialog are syncing from props.
  //   - react-hooks/refs fires on use-subscription.ts writing onDataRef during
  //     render. That is deliberate and documented there — it keeps a changing
  //     callback from reopening the socket — so this one may well end up an
  //     explicit disable rather than a change.
  //   - only-export-components wants credential-validity-utils.tsx split into a
  //     module of components and a module of helpers; mechanical, but it touches
  //     every importer.
  {
    files: [
      'src/components/findings/hash-details-dialog.tsx',
      'src/components/ui/search-input.tsx',
      'src/graphql/hooks/timeline.ts',
      'src/hooks/use-mobile.ts',
      'src/lib/sha1.ts',
    ],
    rules: {
      'react-hooks/set-state-in-effect': 'warn',
    },
  },
  {
    files: ['src/hooks/use-subscription.ts'],
    rules: {
      'react-hooks/refs': 'warn',
    },
  },
  {
    files: ['src/components/findings/credential-validity-utils.tsx'],
    rules: {
      'react-refresh/only-export-components': 'warn',
    },
  },
])

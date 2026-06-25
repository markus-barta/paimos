// PAI-295 — real lint gate beyond vue-tsc. eslint (flat config) with Vue 3
// essential rules + the typescript-eslint recommended set; prettier owns
// formatting (eslint-config-prettier disables stylistic rules so the two never
// fight). The gate fails on errors; a few high-volume rules are warnings so the
// first adoption catches real bugs without a repo-wide refactor.
import pluginVue from 'eslint-plugin-vue'
import { defineConfigWithVueTs, vueTsConfigs } from '@vue/eslint-config-typescript'
import eslintConfigPrettier from 'eslint-config-prettier'

export default defineConfigWithVueTs(
  {
    name: 'app/ignores',
    ignores: [
      'dist/**',
      'coverage/**',
      'node_modules/**',
      'src/types/generated/**', // generated from the Go schema
      'scripts/.visual-tooling/**',
      '*.config.{js,ts,mjs,cjs}',
    ],
  },
  {
    name: 'app/files',
    files: ['**/*.{ts,mts,tsx,vue}'],
  },
  pluginVue.configs['flat/essential'],
  vueTsConfigs.recommended,
  eslintConfigPrettier,
  {
    name: 'app/rules',
    rules: {
      'vue/multi-word-component-names': 'off',
      '@typescript-eslint/no-explicit-any': 'warn',
      // PAI-295 — first adoption: the gate enforces all the *bug* rules
      // (no-undef, valid-v-for, no-dupe-args, …) as errors so new violations
      // fail CI; the high-volume EXISTING debt below is surfaced as warnings to
      // burn down incrementally rather than block this PR on a repo-wide sweep.
      // `_`-prefixed args/vars stay the intentional-unused convention.
      '@typescript-eslint/no-unused-vars': [
        'warn',
        {
          argsIgnorePattern: '^_',
          varsIgnorePattern: '^_',
          caughtErrorsIgnorePattern: '^_',
        },
      ],
      'vue/no-mutating-props': 'warn',
      'vue/no-dupe-keys': 'warn', // <script setup> ref names — false-positive-prone
    },
  },
)

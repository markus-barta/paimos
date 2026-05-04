/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public
 * License along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

import { defineConfig, mergeConfig } from 'vitest/config'
import viteConfig from './vite.config'

// Vitest 3 + happy-dom. Config merges the existing Vite config so path aliases
// (@/), plugins, and define: flags all flow through unchanged.
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      environment: 'happy-dom',
      globals: true,
      include: ['src/**/*.{test,spec}.ts'],
      setupFiles: ['src/test/setup.ts'],
      // Keep the suite quick: no coverage by default, parallel workers.
      reporters: process.env.CI ? ['default', 'junit'] : ['default'],
      outputFile: process.env.CI ? { junit: 'test-results/frontend-junit.xml' } : undefined,
    },
  }),
)

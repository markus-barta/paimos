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

import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import i18n from './i18n'
import { vAutoGrow } from '@/directives/autoGrow'
import { useBranding } from '@/composables/useBranding'
import { syncSidebarWithBranding } from '@/composables/useSidebarColors'

// PAI-118: bundle fonts at build time so the SPA never makes a runtime
// request to fonts.googleapis.com / fonts.gstatic.com. Each weight is a
// separate import so Vite can drop unused weights from the bundle.
import '@fontsource/bricolage-grotesque/400.css'
import '@fontsource/bricolage-grotesque/500.css'
import '@fontsource/bricolage-grotesque/600.css'
import '@fontsource/bricolage-grotesque/700.css'
import '@fontsource/jetbrains-mono/400.css'
import '@fontsource/jetbrains-mono/500.css'
import '@fontsource/jetbrains-mono/600.css'
import '@fontsource/dm-sans/300.css'
import '@fontsource/dm-sans/400.css'
import '@fontsource/dm-sans/500.css'
import '@fontsource/dm-sans/600.css'
import '@fontsource/dm-sans/700.css'

// Load branding config before mounting so CSS vars and title are set early
const { init: initBranding } = useBranding()
initBranding().then(() => {
  syncSidebarWithBranding()
  const app = createApp(App)
  app.use(createPinia())
  app.use(router)
  app.use(i18n)
  app.directive('auto-grow', vAutoGrow)
  app.mount('#app')
})

<script setup lang="ts">
import { ref } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useSearchStore } from '@/stores/search'
import { useRouter } from 'vue-router'
import AppIcon from '@/components/AppIcon.vue'
import { useBranding } from '@/composables/useBranding'

const { branding } = useBranding()

const auth = useAuthStore()
const search = useSearchStore()
const router = useRouter()

const searchFocused = ref(false)

function onSearchInput() {
  search.setQuery(search.query.trim())
}

function clearSearch() {
  search.clear()
}

async function logout() {
  await auth.logout()
}
</script>

<template>
  <div class="portal-shell">
    <header class="portal-header">
      <div class="portal-header-left">
        <img :src="branding.logo" alt="" class="portal-logo" />
        <span class="portal-brand">{{ branding.company }}</span>
        <span class="portal-sep">|</span>
        <router-link to="/portal" class="portal-nav-link">{{ $t('portal.title') }}</router-link>
      </div>
      <div class="portal-header-center">
        <div :class="['portal-search-wrap', { focused: searchFocused }]">
          <AppIcon name="search" :size="13" class="portal-search-icon" />
          <input
            v-model="search.query"
            type="search"
            class="portal-search-input"
            :placeholder="$t('portal.search')"
            autocomplete="off"
            spellcheck="false"
            @focus="searchFocused = true"
            @blur="searchFocused = false"
            @input="onSearchInput"
          />
          <button v-if="search.query" class="portal-search-clear" @mousedown.prevent="clearSearch">
            <AppIcon name="x" :size="12" />
          </button>
        </div>
      </div>
      <div class="portal-header-right">
        <span class="portal-user">{{ auth.user?.username }}</span>
        <button class="btn btn-ghost btn-sm" @click="logout">{{ $t('portal.logout') }}</button>
      </div>
    </header>
    <main class="portal-main">
      <slot />
    </main>
    <footer class="portal-footer">
      <img :src="branding.logo" alt="" class="portal-footer-logo" aria-hidden="true" />
      <span>{{ branding.company }}</span>
    </footer>
  </div>
</template>

<style scoped>
.portal-shell {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  background: var(--bg);
}
.portal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: .75rem 1.5rem;
  background: var(--bg-card);
  border-bottom: 1px solid var(--border);
  box-shadow: var(--shadow);
  position: sticky;
  top: 0;
  z-index: 100;
}
.portal-header-left {
  display: flex;
  align-items: center;
  gap: .75rem;
}
.portal-logo {
  height: 24px;
  width: auto;
}
.portal-brand {
  font-size: 13px;
  font-weight: 700;
  letter-spacing: .06em;
  text-transform: uppercase;
  color: var(--text);
}
.portal-sep {
  color: var(--border);
  font-size: 16px;
}
.portal-nav-link {
  font-size: 14px;
  font-weight: 600;
  color: var(--bp-blue);
}
.portal-header-center { display: flex; justify-content: center; flex: 1; }
.portal-search-wrap {
  position: relative; display: flex; align-items: center;
  width: 260px; transition: width .2s;
}
.portal-search-wrap.focused { width: 340px; }
.portal-search-icon { position: absolute; left: 9px; color: var(--text-muted); pointer-events: none; }
.portal-search-input {
  width: 100%; height: 32px; padding: 0 28px 0 30px;
  border: 1px solid var(--border); border-radius: 20px;
  background: var(--bg); font-size: 13px; font-family: inherit;
  color: var(--text); outline: none;
  transition: border-color .15s, background .15s;
  -webkit-appearance: none;
}
.portal-search-wrap.focused .portal-search-input { border-color: var(--bp-blue); background: var(--bg-card); }
.portal-search-input::-webkit-search-cancel-button { display: none; }
.portal-search-clear {
  position: absolute; right: 8px; background: none; border: none;
  padding: 2px; cursor: pointer; color: var(--text-muted);
  display: flex; align-items: center; border-radius: 50%;
}
.portal-search-clear:hover { color: var(--text); background: var(--bg); }
.portal-header-right {
  display: flex;
  align-items: center;
  gap: .75rem;
}
.portal-user {
  font-size: 13px;
  color: var(--text-muted);
  font-weight: 500;
}
.btn-sm {
  padding: .3rem .65rem;
  font-size: 12px;
}
.portal-main {
  flex: 1;
  padding: 1.5rem;
  max-width: 1200px;
  margin: 0 auto;
  width: 100%;
}
.portal-footer {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: .5rem;
  padding: 1.25rem 0 .5rem;
  margin-top: 2rem;
  border-top: 1px solid var(--border);
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: .06em;
  color: var(--text-muted);
  opacity: .5;
}
.portal-footer-logo {
  height: 16px;
  width: auto;
}
</style>

<script setup lang="ts">
import LoadingText from "@/components/LoadingText.vue";
import { onMounted, ref } from "vue";
import { api, errMsg } from "@/api/client";

interface SystemSettings {
  undo_stack_depth: number;
}

interface RetentionPolicy {
  mutation_log_days: number;
}

const loading = ref(true);
const saving = ref(false);
const error = ref("");
const saved = ref(false);
const form = ref<SystemSettings>({ undo_stack_depth: 3 });
const retention = ref<RetentionPolicy | null>(null);

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const [settings, retentionPolicy] = await Promise.all([
      api.get<SystemSettings>("/system/settings"),
      api.get<RetentionPolicy>("/gdpr/retention"),
    ]);
    form.value = settings;
    retention.value = retentionPolicy;
  } catch (e) {
    error.value = errMsg(e, "Failed to load system settings.");
  } finally {
    loading.value = false;
  }
}

async function save() {
  saving.value = true;
  error.value = "";
  saved.value = false;
  try {
    form.value = await api.put<SystemSettings>("/system/settings", form.value);
    saved.value = true;
  } catch (e) {
    error.value = errMsg(e, "Failed to save system settings.");
  } finally {
    saving.value = false;
  }
}

onMounted(() => {
  void load();
});
</script>

<template>
  <section class="system-tab">
    <header class="system-tab__hero">
      <div>
        <h2>System</h2>
        <p>Runtime controls for the undo stack and its audit retention.</p>
      </div>
    </header>

    <LoadingText v-if="loading" class="system-tab__state" label="Loading…" />
    <div v-else class="system-tab__grid">
      <article class="system-card">
        <h3>Undo stack depth</h3>
        <p class="system-card__copy">
          How many recent actions a user can undo. Bulk operations count as one
          slot.
        </p>
        <label class="system-card__field">
          <span>Depth (1–20)</span>
          <input
            v-model.number="form.undo_stack_depth"
            type="number"
            min="1"
            max="20"
          />
        </label>
        <div class="system-card__meta">
          <span>Applies to the next recorded mutation immediately.</span>
        </div>
      </article>

      <article class="system-card">
        <h3>Audit retention</h3>
        <p class="system-card__copy">
          Undoability and audit existence are separate. Old rows age out via the
          retention sweeper.
        </p>
        <div class="system-card__retention">
          <strong>{{ retention?.mutation_log_days ?? "—" }} days</strong>
          <span
            >Environment: <code>PAIMOS_RETENTION_DAYS_MUTATION_LOG</code></span
          >
        </div>
      </article>
    </div>

    <div v-if="error" class="system-tab__state system-tab__state--error">
      {{ error }}
    </div>
    <div class="system-tab__actions">
      <span v-if="saved" class="system-tab__saved">Saved.</span>
      <button
        type="button"
        class="btn btn-primary"
        :disabled="saving"
        @click="save"
      >
        {{ saving ? "Saving…" : "Save system settings" }}
      </button>
    </div>
  </section>
</template>

<style scoped>
.system-tab {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
.system-tab__hero {
  padding: 1.1rem 1.2rem;
  border: 1px solid rgba(46, 109, 164, 0.14);
  border-radius: 18px;
  background:
    radial-gradient(
      circle at top right,
      rgba(46, 109, 164, 0.14),
      transparent 38%
    ),
    linear-gradient(
      180deg,
      rgba(255, 255, 255, 0.98),
      rgba(242, 245, 248, 0.95)
    );
}
.system-tab__hero h2 {
  font-family: "Bricolage Grotesque", serif;
  font-size: 1.35rem;
}
.system-tab__hero p,
.system-card__copy,
.system-tab__state,
.system-card__meta,
.system-tab__saved {
  color: var(--text-muted);
}
.system-tab__grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 1rem;
}
.system-card {
  padding: 1rem;
  border-radius: 16px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  box-shadow: 0 12px 30px rgba(30, 50, 80, 0.06);
}
.system-card h3 {
  margin-bottom: 0.3rem;
}
.system-card__field {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  margin-top: 0.9rem;
}
.system-card__retention {
  display: flex;
  flex-direction: column;
  gap: 0.2rem;
  margin-top: 1rem;
}
.system-card__retention strong {
  font-family: "DM Mono", "JetBrains Mono", monospace;
  font-size: 1rem;
}
.system-card__retention code {
  font-family: "DM Mono", "JetBrains Mono", monospace;
  font-size: 12px;
}
.system-tab__state--error {
  color: #8f2d23;
}
.system-tab__actions {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 0.7rem;
}
</style>

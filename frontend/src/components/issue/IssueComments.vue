<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { errMsg } from '@/api/client'
import { escapeHtml } from '@/utils/html'
import { useAuthStore } from '@/stores/auth'
import { useConfirm } from '@/composables/useConfirm'
import { fmtShortDateTime } from '@/utils/formatTime'
import { formatInteger } from '@/composables/useNumberFormat'
import AppIcon from '@/components/AppIcon.vue'
import UserAvatar from '@/components/UserAvatar.vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { vAutoGrow } from '@/directives/autoGrow'
import {
  createIssueComment,
  deleteIssueComment,
  loadIssueComments,
  updateIssueCommentVisibility,
  type CommentVisibility,
  type IssueComment as Comment,
} from '@/services/issueComments'

const props = defineProps<{
  issueId: number
  mdMode: boolean
  isMonospace: boolean
}>()

const authStore = useAuthStore()
const { confirm } = useConfirm()
const { t } = useI18n()

const comments      = ref<Comment[]>([])
const commentBody   = ref('')
const commentSaving = ref(false)
const commentError  = ref('')
// PAI-475: visibility on the composer. Safe-by-default — 'internal' is
// preselected; the team must explicitly opt in to send a comment to the
// Customer Portal. The choice resets after every successful post so the
// next comment doesn't accidentally inherit the previous selection.
const commentVisibility = ref<CommentVisibility>('internal')

async function load() {
  try { comments.value = await loadIssueComments(props.issueId) } catch {}
}

defineExpose({ load })

watch(() => props.issueId, () => load())

function escapeHtmlBr(s: string): string {
  return escapeHtml(s, true)
}

function sanitiseComment(s: string): string {
  const html = marked.parse(s ?? '') as string
  return DOMPurify.sanitize(html)
}

async function submitComment() {
  commentError.value = ''
  if (!commentBody.value.trim()) return
  commentSaving.value = true
  try {
    const c = await createIssueComment(
      props.issueId,
      commentBody.value.trim(),
      commentVisibility.value,
    )
    comments.value.push(c)
    commentBody.value = ''
    // Reset to internal: the safe default for the next comment, even
    // if the previous one was customer-visible.
    commentVisibility.value = 'internal'
  } catch (e: unknown) { commentError.value = errMsg(e, 'Failed to post comment.') }
  finally { commentSaving.value = false }
}

async function deleteComment(comment: Comment) {
  const isOther = comment.author_id !== authStore.user?.id
  const msg = isOther
    ? `Delete ${comment.author ?? 'another user'}'s comment? You can undo this from Recent activity.`
    : 'Delete this comment? You can undo this from Recent activity.'
  if (!await confirm({ message: msg, confirmLabel: 'Delete', danger: true })) return
  await deleteIssueComment(comment.id)
  comments.value = comments.value.filter(c => c.id !== comment.id)
}

// Author or admin can flip a comment's visibility post-hoc — useful when
// somebody composed an internal answer and then realised the customer
// should see it (or vice versa).
function canFlipVisibility(comment: Comment): boolean {
  if (!authStore.user) return false
  return comment.author_id === authStore.user.id || authStore.isAdmin
}

async function flipVisibility(comment: Comment) {
  const next: CommentVisibility =
    comment.visibility === 'external' ? 'internal' : 'external'
  // For internal → external, take a moment to confirm — pushing a
  // comment to the customer is a one-way "they see it" action.
  if (next === 'external') {
    const ok = await confirm({
      message: t('comments.visibility.composerWarning'),
      confirmLabel: t('comments.visibility.makeExternal'),
    })
    if (!ok) return
  }
  try {
    await updateIssueCommentVisibility(comment.id, next)
    comment.visibility = next
  } catch (e: unknown) {
    commentError.value = errMsg(e, 'Failed to update visibility.')
  }
}
</script>

<template>
  <div class="comments-section">
    <h3 class="comments-title">Comments <span class="comments-count" v-if="comments.length">{{ formatInteger(comments.length) }}</span></h3>

    <div v-if="comments.length" class="comments-list">
      <div
        v-for="c in comments"
        :key="c.id"
        :class="['comment', { 'comment--external': c.visibility === 'external' }]"
      >
        <UserAvatar :user="{ username: c.author ?? '?', avatar_path: c.avatar_path ?? undefined }" size="md" class="comment-avatar-ua" />
        <div class="comment-body-wrap">
          <div class="comment-meta">
            <span class="comment-author">{{ c.author ?? 'deleted user' }}</span>
            <span class="comment-date">{{ fmtShortDateTime(c.created_at) }}</span>
            <!-- PAI-475: visibility badge — always rendered so the team
                 can see at a glance which thread items are customer-facing. -->
            <button
              type="button"
              :class="['comment-vis-badge', `comment-vis-badge--${c.visibility}`]"
              :title="canFlipVisibility(c)
                ? (c.visibility === 'external'
                    ? t('comments.visibility.makeInternal')
                    : t('comments.visibility.makeExternal'))
                : ''"
              :disabled="!canFlipVisibility(c)"
              @click="canFlipVisibility(c) && flipVisibility(c)"
            >
              {{ c.visibility === 'external'
                ? t('comments.visibility.badgeExternal')
                : t('comments.visibility.badgeInternal') }}
            </button>
            <button
              v-if="authStore.user && (c.author_id === authStore.user.id || authStore.isAdmin)"
              class="comment-delete" @click="deleteComment(c)" title="Delete comment"
            ><AppIcon name="x" :size="11" /></button>
          </div>
          <div
            :class="['comment-text', { 'comment-text--mono': isMonospace, 'comment-text--md': mdMode }]"
            v-html="mdMode ? sanitiseComment(c.body) : escapeHtmlBr(c.body)"
          />
        </div>
      </div>
    </div>

    <div class="comment-form">
      <UserAvatar :user="authStore.user" size="md" class="comment-avatar-ua comment-avatar-self" />
      <div class="comment-input-wrap">
        <textarea
          v-auto-grow
          v-model="commentBody"
          :class="[
            'comment-textarea',
            { 'textarea--mono': isMonospace, 'comment-textarea--external': commentVisibility === 'external' },
          ]"
          placeholder="Write something… (Ctrl+Enter to post)"
          rows="2"
          @keydown.ctrl.enter="submitComment"
          @keydown.meta.enter="submitComment"
        ></textarea>
        <!-- PAI-475: visibility chooser + helper text. The internal/
             external chips behave like a radio group; the helper line
             swaps copy + color when external is selected so the author
             cannot accidentally publish to the customer portal. -->
        <div class="comment-vis-row" role="radiogroup" :aria-label="t('comments.visibility.toggleAriaLabel')">
          <button
            type="button"
            role="radio"
            :aria-checked="commentVisibility === 'internal'"
            :class="['comment-vis-pill', { 'comment-vis-pill--active': commentVisibility === 'internal' }]"
            @click="commentVisibility = 'internal'"
          >
            {{ t('comments.visibility.badgeInternal') }}
          </button>
          <button
            type="button"
            role="radio"
            :aria-checked="commentVisibility === 'external'"
            :class="[
              'comment-vis-pill',
              'comment-vis-pill--external',
              { 'comment-vis-pill--active': commentVisibility === 'external' },
            ]"
            @click="commentVisibility = 'external'"
          >
            {{ t('comments.visibility.badgeExternal') }}
          </button>
          <span
            :class="['comment-vis-hint', { 'comment-vis-hint--warn': commentVisibility === 'external' }]"
          >
            {{ commentVisibility === 'external'
              ? t('comments.visibility.composerWarning')
              : t('comments.visibility.composerHint') }}
          </span>
        </div>
        <div class="comment-form-actions">
          <span v-if="commentError" class="comment-error">{{ commentError }}</span>
          <button class="btn btn-primary btn-sm" :disabled="commentSaving || !commentBody.trim()" @click="submitComment">
            {{ commentSaving ? 'Posting…' : 'Post' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.comments-section {
  margin-top: 1.75rem;
  padding-top: 1.5rem;
  border-top: 1px solid var(--border);
}
.comments-title {
  font-size: 13px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .06em; color: var(--text-muted);
  margin-bottom: 1rem; display: flex; align-items: center; gap: .5rem;
}
.comments-count {
  background: var(--bg); border: 1px solid var(--border);
  border-radius: 20px; font-size: 11px; padding: .05rem .45rem;
  font-weight: 600; color: var(--text-muted);
}
.comments-list { display: flex; flex-direction: column; gap: 1rem; margin-bottom: 1.25rem; }
.comment { display: flex; gap: .75rem; align-items: flex-start; }
.comment-avatar-ua { flex-shrink: 0; width: 24px !important; height: 24px !important; }
.comment-avatar-self { background: var(--bp-blue) !important; color: #fff !important; }
.comment-body-wrap { flex: 1; min-width: 0; }
.comment-meta { display: flex; align-items: center; gap: .5rem; margin-bottom: .3rem; }
.comment-author { font-size: 13px; font-weight: 600; color: var(--text); }
.comment-date   { font-size: 11px; color: var(--text-muted); }
.comment-delete {
  margin-left: auto; background: none; border: none; cursor: pointer;
  color: var(--text-muted); font-size: 16px; line-height: 1; padding: 0 .2rem;
  border-radius: 3px;
}
.comment-delete:hover { color: #c0392b; }
.comment-text { font-size: 13px; color: var(--text); line-height: 1.6; white-space: pre-wrap; }
.comment-text--mono { font-family: 'DM Mono', 'Menlo', monospace; font-size: 12px; }
.comment-text--md   { white-space: normal; }
.comment-text--md :deep(h1),.comment-text--md :deep(h2),.comment-text--md :deep(h3) { font-weight:700; margin:.5rem 0 .25rem; }
.comment-text--md :deep(p)  { margin:0 0 .4rem; }
.comment-text--md :deep(ul),.comment-text--md :deep(ol) { padding-left:1.2rem; margin:0 0 .4rem; }
.comment-text--md :deep(li:has(> input[type='checkbox'])) { list-style: none; margin-left: -1.2rem; }
.comment-text--md :deep(li > input[type='checkbox']) {
  width: auto; padding: 0; border: revert; border-radius: 0; background: revert;
  margin-right: .4rem; vertical-align: middle; display: inline; cursor: default;
}
.comment-text--md :deep(code) { font-family:'DM Mono',monospace; font-size:11px; background:var(--bg); padding:.1rem .25rem; border-radius:3px; }
.comment-text--md :deep(pre) { background:var(--bg); border:1px solid var(--border); border-radius:var(--radius); padding:.5rem .75rem; overflow-x:auto; margin:.4rem 0; }
.comment-text--md :deep(pre code) { background:none; padding:0; }
.comment-text--md :deep(a) { color:var(--bp-blue); text-decoration:underline; }
.textarea--mono { font-family: 'DM Mono', 'Menlo', monospace !important; font-size: 13px; }

.comment-form { display: flex; gap: .75rem; align-items: flex-start; }
.comment-input-wrap { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: .4rem; }
.comment-textarea { font-size: 13px; resize: vertical; min-height: 60px; }
/* PAI-475: clear visual cue when authoring a customer-visible comment. */
.comment-textarea--external {
  border-color: var(--bp-blue, #2563eb);
  box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.08);
}
.comment-form-actions { display: flex; align-items: center; justify-content: flex-end; gap: .5rem; }
.comment-error { font-size: 12px; color: #c0392b; }

/* PAI-475: visibility chips in the composer + per-comment badge. */
.comment-vis-row {
  display: flex;
  align-items: center;
  gap: .4rem;
  flex-wrap: wrap;
  font-size: 11px;
}
.comment-vis-pill {
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 999px;
  padding: .15rem .55rem;
  font-size: 11px;
  font-weight: 500;
  color: var(--text-muted);
  cursor: pointer;
  line-height: 1.5;
}
.comment-vis-pill:hover { color: var(--text); }
.comment-vis-pill--active {
  background: var(--bg-subtle, #f3f4f6);
  color: var(--text);
  border-color: var(--text-muted);
}
.comment-vis-pill--external.comment-vis-pill--active {
  background: rgba(37, 99, 235, 0.08);
  border-color: var(--bp-blue, #2563eb);
  color: var(--bp-blue, #2563eb);
}
.comment-vis-hint {
  font-size: 11px;
  color: var(--text-muted);
}
.comment-vis-hint--warn {
  color: var(--bp-blue, #2563eb);
  font-weight: 500;
}

.comment-vis-badge {
  margin-left: .25rem;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 999px;
  padding: .05rem .4rem;
  font-size: 10.5px;
  font-weight: 500;
  color: var(--text-muted);
  cursor: default;
  line-height: 1.5;
}
.comment-vis-badge--external {
  background: rgba(37, 99, 235, 0.08);
  border-color: var(--bp-blue, #2563eb);
  color: var(--bp-blue, #2563eb);
}
.comment-vis-badge:not(:disabled) { cursor: pointer; }
.comment-vis-badge:not(:disabled):hover { filter: brightness(0.95); }
.comment--external .comment-body-wrap {
  /* subtle left rail picks out customer-visible items in the thread */
  border-left: 2px solid var(--bp-blue, #2563eb);
  padding-left: .55rem;
  margin-left: -.55rem;
}
</style>

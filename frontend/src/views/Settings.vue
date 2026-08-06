<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import {
  Send,
  Server,
  ScrollText,
  Save,
  RefreshCw,
  Plus,
  X,
  LoaderCircle,
  AlertTriangle
} from 'lucide-vue-next'
import { toast } from '../composables/toast'
import { getConfig, updateConfig, reloadConfig } from '../api/config'
import type { ConfigResponse } from '../api/config'

const activeSection = ref<'request' | 'relay' | 'log'>('request')

const sections = [
  { id: 'request', label: '请求设置', icon: Send },
  { id: 'relay', label: '中转站', icon: Server },
  { id: 'log', label: '日志', icon: ScrollText }
] as const

const loading = ref(true)
const saving = ref(false)
const reloading = ref(false)
const loadError = ref('')

type EditableConfig = Omit<ConfigResponse, 'listen_addr' | 'db_path' | 'shutdown_timeout'>

const form = reactive<EditableConfig>({
  log_level: 'info',
  backend_cooldown: '20m',
  backend_fails: '3',
  backend_console_user_agent: 'Red-Token/1.0',
  focus_models: '',
  request_timeout: '2m'
})

const focusModelInput = ref('')
const focusModelTags = computed(() =>
  form.focus_models ? form.focus_models.split(/[,\s]+/).map(s => s.trim()).filter(Boolean) : []
)

function addFocusModel() {
  const name = focusModelInput.value.trim()
  if (!name) return
  const tags = focusModelTags.value
  if (!tags.includes(name)) {
    form.focus_models = [...tags, name].join(',')
  }
  focusModelInput.value = ''
}

function removeFocusModel(name: string) {
  form.focus_models = focusModelTags.value.filter(t => t !== name).join(',')
}

async function loadData() {
  loading.value = true
  loadError.value = ''
  try {
    const cfg = await getConfig()
    form.log_level = cfg.log_level
    form.backend_cooldown = cfg.backend_cooldown
    form.backend_fails = cfg.backend_fails
    form.backend_console_user_agent = cfg.backend_console_user_agent
    form.focus_models = cfg.focus_models
    form.request_timeout = cfg.request_timeout
  } catch (e: any) {
    loadError.value = e?.message || '加载配置失败'
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  try {
    const cfg = await updateConfig({ ...form })
    form.log_level = cfg.log_level
    form.backend_cooldown = cfg.backend_cooldown
    form.backend_fails = cfg.backend_fails
    form.backend_console_user_agent = cfg.backend_console_user_agent
    form.focus_models = cfg.focus_models
    form.request_timeout = cfg.request_timeout
    toast('配置已保存', '日志级别立即生效，其他字段已更新', 'success')
  } catch (e: any) {
    toast('保存失败', e?.message || '', 'error')
  } finally {
    saving.value = false
  }
}

async function reload() {
  reloading.value = true
  try {
    await reloadConfig()
    toast('配置已重新加载', '运行时参数已从数据库刷新', 'success')
    await loadData()
  } catch (e: any) {
    toast('重新加载失败', e?.message || '', 'error')
  } finally {
    reloading.value = false
  }
}

onMounted(loadData)
</script>

<template>
  <div class="page stagger">
    <section class="status-bar panel">
      <span v-if="loadError" class="sb-error"><AlertTriangle :size="14" /> {{ loadError }}</span>
      <span v-else class="sb-note">配置修改后点击「保存」生效；`log_level` 立即生效，其他字段建议重载</span>
      <div class="spacer"></div>
      <div class="sb-actions">
        <button class="btn btn-ghost btn-sm" :disabled="reloading || loading" @click="reload">
          <LoaderCircle v-if="reloading" :size="14" class="spin" />
          <RefreshCw v-else :size="14" />
          重新加载
        </button>
        <button class="btn btn-primary btn-sm" :disabled="saving || loading" @click="save">
          <LoaderCircle v-if="saving" :size="14" class="spin" />
          <Save v-else :size="14" />
          保存
        </button>
      </div>
    </section>

    <div v-if="loading" class="cfg-loading">
      <LoaderCircle :size="20" class="spin" /><span>正在加载配置…</span>
    </div>

    <div v-else class="settings-layout">
      <aside class="sections panel">
        <button
          v-for="s in sections"
          :key="s.id"
          class="section-btn"
          :class="{ on: activeSection === s.id }"
          @click="activeSection = s.id"
        >
          <component :is="s.icon" :size="16" />
          <span>{{ s.label }}</span>
          <i v-if="activeSection === s.id"></i>
        </button>
      </aside>

      <main class="settings-content">
        <!-- 请求设置 -->
        <section v-if="activeSection === 'request'" class="panel content-panel">
          <div class="panel-header">
            <div class="panel-title"><Send :size="16" /> 请求设置</div>
            <div class="panel-sub">出站请求的超时与客户端标识</div>
          </div>
          <div class="panel-body">
            <div class="field">
              <label class="field-label">请求超时</label>
              <input v-model="form.request_timeout" class="input mono" placeholder="如 2m、90s" />
              <span class="field-hint">Go Duration 格式，例如 2m、90s、1m30s</span>
            </div>
            <div class="field">
              <label class="field-label">控制台 User-Agent</label>
              <input v-model="form.backend_console_user_agent" class="input mono" placeholder="Red-Token/1.0" />
              <span class="field-hint">签到/同步请求携带的客户端标识，最多 512 字符</span>
            </div>
          </div>
        </section>

        <!-- 中转站 -->
        <section v-else-if="activeSection === 'relay'" class="panel content-panel">
          <div class="panel-header">
            <div class="panel-title"><Server :size="16" /> 中转站</div>
            <div class="panel-sub">失败熔断、自动恢复与关注模型</div>
          </div>
          <div class="panel-body">
            <div class="field">
              <label class="field-label">最大失败次数</label>
              <input v-model="form.backend_fails" class="input mono" placeholder="3" />
              <span class="field-hint">连续失败达到该次数后将中转站标记为不可用</span>
            </div>
            <div class="field">
              <label class="field-label">冷却时长</label>
              <input v-model="form.backend_cooldown" class="input mono" placeholder="20m" />
              <span class="field-hint">Go Duration 格式，例如 20m、1h；不可用中转站经过该时长后自动尝试恢复</span>
            </div>
            <div class="field">
              <label class="field-label">关注模型</label>
              <div class="watch-input-row">
                <input
                  v-model="focusModelInput"
                  class="input mono"
                  placeholder="输入模型名或匹配模式，回车添加，如 gpt-4.*"
                  @keydown.enter.prevent="addFocusModel"
                />
                <button class="btn btn-ghost" title="添加" @click="addFocusModel"><Plus :size="15" /></button>
              </div>
              <div class="watch-tags">
                <span v-for="m in focusModelTags" :key="m" class="tag">
                  {{ m }}
                  <button class="watch-x" @click="removeFocusModel(m)"><X :size="11" /></button>
                </span>
                <span v-if="!focusModelTags.length" class="watch-empty">尚未设置关注模型</span>
              </div>
              <span class="field-hint">逗号分隔的模型名或正则模式，用于中转站可用性巡检</span>
            </div>
          </div>
        </section>

        <!-- 日志 -->
        <section v-else class="panel content-panel">
          <div class="panel-header">
            <div class="panel-title"><ScrollText :size="16" /> 日志</div>
            <div class="panel-sub">日志级别，保存后立即生效</div>
          </div>
          <div class="panel-body">
            <div class="field">
              <label class="field-label">日志级别</label>
              <select v-model="form.log_level" class="select">
                <option value="debug">debug</option>
                <option value="info">info</option>
                <option value="warn">warn</option>
                <option value="error">error</option>
              </select>
              <span class="field-hint">保存后立即更新运行时日志级别</span>
            </div>
          </div>
        </section>
      </main>
    </div>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: var(--space-4); }

.status-bar { display: flex; align-items: center; gap: var(--space-4); padding: 12px 16px; flex-wrap: wrap; }
.sb-note { font-size: 12.5px; color: var(--text-faint); }
.sb-error { font-size: 12.5px; color: var(--danger); display: flex; align-items: center; gap: 6px; }
.spacer { flex: 1; }
.sb-actions { display: flex; align-items: center; gap: 10px; }

.cfg-loading { display: flex; align-items: center; gap: 10px; padding: 48px; color: var(--text-faint); font-size: 13px; justify-content: center; }

.settings-layout { display: grid; grid-template-columns: 210px 1fr; gap: var(--space-4); align-items: start; }
.sections { padding: 10px; display: flex; flex-direction: column; gap: 4px; position: sticky; top: calc(var(--header-h) + 18px); }
.section-btn {
  position: relative;
  display: flex; align-items: center; gap: 11px;
  width: 100%; padding: 11px 13px;
  border: none; background: transparent;
  color: var(--text-muted); font-size: 13px; font-weight: 600;
  border-radius: 11px; cursor: pointer;
  transition: all 0.2s var(--ease-out);
  font-family: var(--font-body);
}
.section-btn:hover { color: var(--text); background: var(--surface); }
.section-btn.on { color: var(--text); background: var(--grad-soft); }
.section-btn.on svg { color: var(--primary); }
.section-btn i {
  position: absolute; right: 10px; top: 50%; transform: translateY(-50%);
  width: 6px; height: 6px; border-radius: 50%;
  background: var(--primary); box-shadow: 0 0 8px var(--primary);
}

.content-panel { min-height: 320px; }
.panel-header { padding: 18px 20px 0; }
.panel-title { display: flex; align-items: center; gap: 8px; font-size: 14px; font-weight: 600; color: var(--text); }
.panel-sub { font-size: 12px; color: var(--text-faint); margin-top: 4px; }
.panel-body { padding: 20px; display: flex; flex-direction: column; gap: 16px; }

.field { display: flex; flex-direction: column; gap: 6px; }
.field-label { font-size: 12.5px; font-weight: 600; color: var(--text-soft); }
.field-hint { font-size: 11.5px; color: var(--text-faint); }

.watch-input-row { display: flex; gap: 8px; }
.watch-input-row .input { flex: 1; }
.watch-tags { display: flex; flex-wrap: wrap; gap: 7px; }
.watch-tags .tag { display: inline-flex; align-items: center; gap: 5px; }
.watch-x {
  display: inline-flex; align-items: center; justify-content: center;
  width: 16px; height: 16px; padding: 0;
  border: none; border-radius: 4px;
  background: transparent; color: var(--text-faint); cursor: pointer;
}
.watch-x:hover { color: var(--danger); background: var(--danger-soft); }
.watch-empty { font-size: 12px; color: var(--text-faint); }

@media (max-width: 900px) {
  .settings-layout { grid-template-columns: 1fr; }
  .sections { position: static; flex-direction: row; overflow-x: auto; }
  .section-btn { flex: none; width: auto; }
  .section-btn i { display: none; }
}
</style>

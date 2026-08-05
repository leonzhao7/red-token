<script setup lang="ts">
import { ref } from 'vue'
import {
  Send,
  Server,
  ScrollText,
  Save,
  RotateCcw,
  Plus,
  X
} from 'lucide-vue-next'
import { toast } from '../composables/toast'
import { MODEL_CATALOG } from '../data/mock'

const activeSection = ref<'request' | 'relay' | 'log'>('request')

const sections = [
  { id: 'request', label: '请求设置', icon: Send },
  { id: 'relay', label: '中转站', icon: Server },
  { id: 'log', label: '日志', icon: ScrollText }
] as const

const form = ref({
  timeout: 60,
  userAgent: 'NexusGateway/2.4.1',
  maxFailures: 3,
  recoverySeconds: 300,
  watchModels: ['gpt-4o', 'deepseek-r1', 'claude-3-5-sonnet'],
  logLevel: 'info'
})

const modelInput = ref('')

const save = () => toast('配置已保存', '新配置将在 30 秒内热加载生效', 'success')
const reset = () => toast('已恢复默认配置', '', 'info')

interface FieldDef {
  id: string
  label: string
  hint: string
  type?: 'number'
  options?: readonly string[]
  mono?: boolean
}

const requestKeys: FieldDef[] = [
  { id: 'timeout', label: '请求超时（秒）', hint: '上游响应超过该时长判定为失败', type: 'number' },
  { id: 'userAgent', label: '请求 User-Agent', hint: '出站请求携带的客户端标识', mono: true }
]

const relayKeys: FieldDef[] = [
  { id: 'maxFailures', label: '最大失败次数', hint: '连续失败达到该次数后将中转站标记为不可用', type: 'number' },
  { id: 'recoverySeconds', label: '恢复时长（秒）', hint: '不可用中转站经过该时长后自动尝试恢复', type: 'number' }
]

const logKeys: FieldDef[] = [
  { id: 'logLevel', label: '日志级别', hint: '控制台与文件日志的详细程度', options: ['debug', 'info', 'warn', 'error'] }
]

function addWatchModel() {
  const name = modelInput.value.trim()
  if (!name) return
  const lower = name.toLowerCase()
  if (!form.value.watchModels.some((m) => m.toLowerCase() === lower)) {
    form.value.watchModels.push(name)
  }
  modelInput.value = ''
}

function removeWatchModel(name: string) {
  const i = form.value.watchModels.indexOf(name)
  if (i >= 0) form.value.watchModels.splice(i, 1)
}
</script>

<template>
  <div class="page stagger">
    <!-- status bar -->
    <section class="status-bar panel">
      <span class="sb-note">配置修改后点击「保存配置」生效，可随时「恢复默认」</span>
      <div class="spacer"></div>
      <div class="sb-actions">
        <button class="btn btn-ghost btn-sm" @click="reset"><RotateCcw :size="14" /> 恢复默认</button>
        <button class="btn btn-primary btn-sm" @click="save"><Save :size="14" /> 保存配置</button>
      </div>
    </section>

    <div class="settings-layout">
      <!-- section nav -->
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

      <!-- content -->
      <main class="settings-content">
        <!-- request -->
        <section v-if="activeSection === 'request'" class="panel content-panel">
          <div class="panel-header">
            <div>
              <div class="panel-title"><Send :size="16" /> 请求设置</div>
              <div class="panel-sub">出站请求的超时与客户端标识</div>
            </div>
          </div>
          <div class="panel-body">
            <div class="form-grid-2">
              <div v-for="k in requestKeys" :key="k.id" class="field">
                <label class="field-label">{{ k.label }}</label>
                <input v-if="k.type === 'number'" v-model.number="(form as any)[k.id]" class="input mono" type="number" />
                <input v-else v-model="(form as any)[k.id]" class="input mono" />
                <span class="field-hint">{{ k.hint }}</span>
              </div>
            </div>
          </div>
        </section>

        <!-- relay -->
        <section v-else-if="activeSection === 'relay'" class="panel content-panel">
          <div class="panel-header">
            <div>
              <div class="panel-title"><Server :size="16" /> 中转站</div>
              <div class="panel-sub">失败熔断、自动恢复与关注模型</div>
            </div>
          </div>
          <div class="panel-body">
            <div class="form-grid-2">
              <div v-for="k in relayKeys" :key="k.id" class="field">
                <label class="field-label">{{ k.label }}</label>
                <input v-model.number="(form as any)[k.id]" class="input mono" type="number" />
                <span class="field-hint">{{ k.hint }}</span>
              </div>
            </div>
            <div class="field">
              <label class="field-label">关注模型列表</label>
              <div class="watch-input-row">
                <input
                  v-model="modelInput"
                  class="input mono"
                  list="watch-model-options"
                  placeholder="输入模型名，回车添加，如 gpt-4o"
                  @keydown.enter.prevent="addWatchModel"
                />
                <datalist id="watch-model-options">
                  <option v-for="m in MODEL_CATALOG" :key="m.id" :value="m.name" />
                </datalist>
                <button class="btn btn-ghost" title="添加模型" @click="addWatchModel"><Plus :size="15" /></button>
              </div>
              <div class="watch-tags">
                <span v-for="m in form.watchModels" :key="m" class="tag">
                  {{ m }}
                  <button class="watch-x" title="移除" @click="removeWatchModel(m)"><X :size="11" /></button>
                </span>
                <span v-if="!form.watchModels.length" class="watch-empty">尚未关注任何模型，请输入模型名添加</span>
              </div>
              <span class="field-hint">勾选的模型参与用量统计与额度巡检，用于中转站可用性分析</span>
            </div>
          </div>
        </section>

        <!-- log -->
        <section v-else class="panel content-panel">
          <div class="panel-header">
            <div>
              <div class="panel-title"><ScrollText :size="16" /> 日志</div>
              <div class="panel-sub">日志记录级别控制</div>
            </div>
          </div>
          <div class="panel-body">
            <div class="form-grid-2">
              <div v-for="k in logKeys" :key="k.id" class="field">
                <label class="field-label">{{ k.label }}</label>
                <select v-model="(form as any)[k.id]" class="select">
                  <option v-for="o in k.options" :key="o" :value="o">{{ o }}</option>
                </select>
                <span class="field-hint">{{ k.hint }}</span>
              </div>
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
.spacer { flex: 1; }
.sb-actions { display: flex; align-items: center; gap: 10px; }

.settings-layout { display: grid; grid-template-columns: 210px 1fr; gap: var(--space-4); align-items: start; }
.sections { padding: 10px; display: flex; flex-direction: column; gap: 4px; position: sticky; top: calc(var(--header-h) + 18px); }
.section-btn {
  position: relative;
  display: flex; align-items: center; gap: 11px;
  width: 100%;
  padding: 11px 13px;
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
  position: absolute; right: 10px; top: 50%;
  transform: translateY(-50%);
  width: 6px; height: 6px; border-radius: 50%;
  background: var(--primary); box-shadow: 0 0 8px var(--primary);
}

.content-panel { min-height: 320px; }
.form-grid-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
.watch-input-row { display: flex; gap: 8px; }
.watch-input-row .input { flex: 1; }
.watch-tags { display: flex; flex-wrap: wrap; gap: 7px; margin-top: 10px; }
.watch-tags .tag { display: inline-flex; align-items: center; gap: 5px; }
.watch-x {
  display: inline-flex; align-items: center; justify-content: center;
  width: 16px; height: 16px; padding: 0;
  border: none; border-radius: 4px;
  background: transparent; color: var(--text-faint);
  cursor: pointer;
}
.watch-x:hover { color: var(--danger); background: var(--danger-soft); }
.watch-empty { font-size: 12px; color: var(--text-faint); }

@media (max-width: 900px) {
  .settings-layout { grid-template-columns: 1fr; }
  .sections { position: static; flex-direction: row; overflow-x: auto; }
  .section-btn { flex: none; width: auto; }
  .section-btn i { display: none; }
  .form-grid-2 { grid-template-columns: 1fr; }
}
</style>

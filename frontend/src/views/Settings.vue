<script setup lang="ts">
import { ref } from 'vue'
import {
  Settings,
  Server,
  Shield,
  Save,
  CheckCircle2,
  RotateCcw,
  Globe,
  Cpu
} from 'lucide-vue-next'
import { toast } from '../composables/toast'

const activeSection = ref<'general' | 'gateway'>('general')

const sections = [
  { id: 'general', label: '通用设置', icon: Settings },
  { id: 'gateway', label: '网关与路由', icon: Server }
] as const

const form = ref({
  systemName: 'NEXUS Relay Gateway',
  environment: 'production',
  timezone: 'Asia/Shanghai',
  language: 'zh-CN',
  retries: 3,
  timeout: 60,
  failover: true,
  strategy: 'priority',
  globalRate: 5000
})

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

const keys: FieldDef[] = [
  { id: 'systemName', label: '系统名称', hint: '网关对外展示名称' },
  { id: 'environment', label: '运行环境', hint: '影响日志级别与调试信息', options: ['production', 'staging', 'development'] },
  { id: 'timezone', label: '默认时区', hint: '日志与报表时区', options: ['Asia/Shanghai', 'UTC', 'America/New_York', 'Europe/Berlin'] },
  { id: 'language', label: '界面语言', hint: '控制台展示语言', options: ['zh-CN', 'en-US'] }
]

const gatewayKeys: FieldDef[] = [
  { id: 'retries', label: '失败重试次数', hint: '单次请求最大重试', type: 'number' },
  { id: 'timeout', label: '超时时间（秒）', hint: '上游响应超时上限', type: 'number' },
  { id: 'strategy', label: '路由策略', hint: '请求如何分配到中转站', options: ['priority', 'weighted', 'latency', 'random'] },
  { id: 'globalRate', label: '全局限流（次/分）', hint: '全网关聚合速率', type: 'number' }
]

</script>

<template>
  <div class="page stagger">
    <!-- status bar -->
    <section class="status-bar panel">
      <div class="sb-cell">
        <span class="sb-ico green"><CheckCircle2 :size="15" /></span>
        <div><span class="sb-label">网关状态</span><span class="sb-val">运行正常</span></div>
      </div>
      <div class="sb-cell">
        <span class="sb-ico cyan"><Globe :size="15" /></span>
        <div><span class="sb-label">API 版本</span><span class="sb-val mono">v1</span></div>
      </div>
      <div class="sb-cell">
        <span class="sb-ico violet"><Cpu :size="15" /></span>
        <div><span class="sb-label">内核版本</span><span class="sb-val mono">2.4.1-core</span></div>
      </div>
      <div class="sb-cell grow">
        <div class="sb-banner">
          <span class="pulse-dot" style="width: 7px;height:7px;border-radius:50%;background:var(--success);display:inline-block"></span>
          配置热更新已开启 · 变更无需重启进程
        </div>
      </div>
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
        <!-- general -->
        <section v-if="activeSection === 'general'" class="panel content-panel">
          <div class="panel-header">
            <div>
              <div class="panel-title"><Settings :size="16" /> 通用设置</div>
              <div class="panel-sub">网关基本信息与本地化选项</div>
            </div>
          </div>
          <div class="panel-body">
            <div class="form-grid-2">
              <div v-for="k in keys" :key="k.id" class="field">
                <label class="field-label">{{ k.label }}</label>
                <input v-if="!('options' in k)" v-model="(form as any)[k.id]" class="input" />
                <select v-else v-model="(form as any)[k.id]" class="select">
                  <option v-for="o in k.options" :key="o" :value="o">{{ o }}</option>
                </select>
                <span class="field-hint">{{ k.hint }}</span>
              </div>
            </div>
          </div>
        </section>

        <!-- gateway -->
        <section v-else-if="activeSection === 'gateway'" class="panel content-panel">
          <div class="panel-header">
            <div>
              <div class="panel-title"><Server :size="16" /> 网关与路由</div>
              <div class="panel-sub">请求分发、重试与故障转移</div>
            </div>
          </div>
          <div class="panel-body">
            <div class="form-grid-2">
              <div v-for="k in gatewayKeys" :key="k.id" class="field">
                <label class="field-label">{{ k.label }}</label>
                <input v-if="k.type === 'number'" v-model.number="(form as any)[k.id]" class="input mono" type="number" />
                <select v-else v-model="(form as any)[k.id]" class="select">
                  <option v-for="o in k.options" :key="o" :value="o">{{ o }}</option>
                </select>
                <span class="field-hint">{{ k.hint }}</span>
              </div>
            </div>
            <div class="switch-row">
              <div>
                <strong>自动故障转移</strong>
                <span class="switch-hint">上游异常时自动切换至备用中转站</span>
              </div>
              <span class="switch" :class="{ on: form.failover }" @click="form.failover = !form.failover"></span>
            </div>
          </div>
        </section>
      </main>
    </div>

    <section class="panel danger-zone">
      <div class="dz-copy">
        <span class="dz-ico"><Shield :size="16" /></span>
        <div>
          <strong>危险操作区</strong>
          <span class="dz-sub">以下操作不可撤销，请谨慎执行</span>
        </div>
      </div>
      <div class="dz-actions">
        <button class="btn btn-outline" @click="toast('操作已取消', '需要二次确认', 'info')">清空缓存</button>
        <button class="btn btn-danger" @click="toast('已重启网关', '控制平面已重启', 'success')">重启网关</button>
      </div>
    </section>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: var(--space-4); }

.status-bar { display: flex; align-items: center; gap: var(--space-4); padding: 14px 18px; flex-wrap: wrap; }
.sb-cell { display: flex; align-items: center; gap: 10px; }
.sb-cell.grow { flex: 1; min-width: 240px; justify-content: flex-end; }
.sb-actions { display: flex; align-items: center; gap: 10px; }
.sb-ico { width: 32px; height: 32px; border-radius: 10px; display: flex; align-items: center; justify-content: center; }
.sb-ico.green { background: var(--success-soft); color: var(--success); }
.sb-ico.cyan { background: rgba(34,211,238,0.12); color: var(--c1); }
.sb-ico.violet { background: rgba(139,92,246,0.12); color: var(--c2); }
.sb-cell div { display: flex; flex-direction: column; }
.sb-label { font-size: 10px; color: var(--text-faint); font-weight: 600; letter-spacing: 0.06em; text-transform: uppercase; }
.sb-val { font-size: 13.5px; font-weight: 600; color: var(--text); }
.sb-banner {
  display: flex; align-items: center; gap: 9px;
  font-size: 12.5px; color: var(--text-soft);
  padding: 9px 14px; border-radius: 10px;
  background: var(--success-soft); border: 1px dashed rgba(52,211,153,0.35);
}

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
.switch-row {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 16px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-md);
  background: var(--surface);
  margin-top: 8px;
}
.switch-row > div { display: flex; flex-direction: column; }
.switch-row strong { font-size: 13px; font-weight: 600; }
.switch-hint { font-size: 11.5px; color: var(--text-faint); }
.secret-input { position: relative; }
.secret-input .icon-btn { position: absolute; right: 6px; top: 50%; transform: translateY(-50%); }
.secret-input .input { padding-right: 42px; }

.danger-zone {
  display: flex; align-items: center; justify-content: space-between;
  padding: 16px 18px; gap: var(--space-4); flex-wrap: wrap;
  border-color: rgba(251,113,133,0.25);
}
.dz-copy { display: flex; align-items: center; gap: 12px; }
.dz-ico {
  width: 34px; height: 34px; border-radius: 11px;
  display: flex; align-items: center; justify-content: center;
  background: var(--danger-soft); color: var(--danger);
}
.dz-copy div { display: flex; flex-direction: column; }
.dz-copy strong { font-size: 13.5px; font-weight: 600; color: var(--danger); }
.dz-sub { font-size: 11.5px; color: var(--text-faint); }
.dz-actions { display: flex; gap: 10px; }

@media (max-width: 900px) {
  .settings-layout { grid-template-columns: 1fr; }
  .sections { position: static; flex-direction: row; overflow-x: auto; }
  .section-btn { flex: none; width: auto; }
  .section-btn i { display: none; }
  .form-grid-2 { grid-template-columns: 1fr; }
}
</style>

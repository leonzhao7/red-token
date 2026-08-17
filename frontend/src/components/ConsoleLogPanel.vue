<script setup lang="ts">
import Modal from './Modal.vue'
import {
  consoleLogVisible,
  consoleLogTitle,
  consoleLogRows,
  consoleLogShowName,
  expandedLogRowIds,
  hideConsoleLog,
  clearConsoleLogRows,
  toggleLogRow,
  formatLogBody,
  formatLogPreview,
  formatLogStatus,
  logStatusClass
} from '../composables/consoleLog'

function toggleDetails(id: string, body: string) {
  if (body) toggleLogRow(id)
}
</script>

<template>
  <Modal
    :open="consoleLogVisible"
    title="控制台请求日志"
    :subtitle="consoleLogTitle"
    width="960px"
    @close="hideConsoleLog"
  >
    <div class="console-log-modal">
      <div class="console-log-toolbar">
        <div class="console-log-context">
          <strong>{{ consoleLogTitle }}</strong>
          <span>{{ consoleLogRows.length }} 条记录</span>
        </div>
        <button class="btn btn-ghost btn-sm" @click="clearConsoleLogRows">
          <svg xmlns="http://www.w3.org/2000/svg" height="16px" viewBox="0 -960 960 960" width="16px" fill="currentColor"><path d="M120-280v-80h560v80H120Zm80-160v-80h560v80H200Zm80-160v-80h560v80H280Z"/></svg>
          Clear
        </button>
      </div>

      <div class="console-log-table-wrap">
        <table class="console-log-table">
          <colgroup>
            <col style="width: 150px" />
            <col style="width: 330px" />
            <col style="width: 100px" />
            <col />
          </colgroup>
          <thead>
            <tr>
              <th>{{ consoleLogShowName ? '中转站' : '时间' }}</th>
              <th>事件</th>
              <th>状态</th>
              <th>详情</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="consoleLogRows.length === 0">
              <td colspan="4" class="console-log-empty">Waiting for request results...</td>
            </tr>
            <template v-for="row in consoleLogRows" :key="row.id">
              <tr
                class="console-log-row"
                :class="{
                  'has-details': Boolean(row.body),
                  'workflow-error': row.kind === 'workflow' && row.level === 'error'
                }"
                @click="toggleDetails(row.id, row.body)"
              >
                <td class="console-log-time">{{ consoleLogShowName ? row.backendName : row.time }}</td>
                <td class="console-log-event">
                  <template v-if="row.kind === 'request'">
                    <span v-if="row.method" class="console-log-method">{{ row.method }}</span>
                    <span class="console-log-path">{{ row.path }}</span>
                  </template>
                  <template v-else>
                    <span class="console-log-workflow-badge">WORKFLOW</span>
                    <span class="console-log-workflow-content">
                      <span class="console-log-workflow-meta">
                        <span v-if="row.stepId" class="mono">{{ row.stepId }}</span>
                        <span class="mono">{{ row.phase }}</span>
                        <span v-if="row.durationMs != null" class="mono">{{ row.durationMs }} ms</span>
                      </span>
                      <span class="console-log-workflow-message">{{ row.message }}</span>
                    </span>
                  </template>
                </td>
                <td>
                  <span v-if="row.kind === 'request'" :class="['console-log-status', logStatusClass(row.statusCode)]">
                    {{ formatLogStatus(row.statusCode) }}
                  </span>
                  <span v-else :class="['console-log-level', `level-${row.level || 'info'}`]">{{ row.level || 'info' }}</span>
                </td>
                <td class="console-log-body-cell">
                  <button
                    v-if="row.body"
                    type="button"
                    class="console-log-body-toggle"
                    @click.stop="toggleLogRow(row.id)"
                  >
                    {{ expandedLogRowIds.has(row.id) ? 'Hide' : 'Show' }}
                  </button>
                  <code v-if="row.body">{{ formatLogPreview(row.body) }}</code>
                  <span v-else class="console-log-no-details">-</span>
                </td>
              </tr>
              <tr v-if="expandedLogRowIds.has(row.id)" class="console-log-expanded">
                <td colspan="4">
                  <pre>{{ formatLogBody(row.body) }}</pre>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
    </div>
  </Modal>
</template>

<style scoped>
.console-log-modal { display: flex; flex-direction: column; gap: 12px; }
.console-log-toolbar {
  display: flex; align-items: center; justify-content: space-between;
  padding: 0 2px;
}
.console-log-context {
  display: flex; align-items: baseline; gap: 10px;
  font-size: 13px; color: var(--text);
}
.console-log-context span { font-size: 11.5px; color: var(--text-faint); }
.console-log-table-wrap {
  border: 1px solid var(--border);
  border-radius: var(--radius-md); background: var(--surface);
  max-height: 500px; overflow-y: auto;
}
.console-log-table { width: 100%; border-collapse: collapse; table-layout: fixed; }
.console-log-table thead {
  background: var(--surface-2); border-bottom: 1px solid var(--border);
  position: sticky; top: 0; z-index: 1;
}
.console-log-table th {
  padding: 10px 12px; text-align: left;
  font-size: 11px; font-weight: 600; color: var(--text-faint);
  text-transform: uppercase; letter-spacing: 0.05em;
}
.console-log-row {
  border-bottom: 1px solid var(--border-soft);
  transition: background 0.15s ease;
}
.console-log-row.has-details { cursor: pointer; }
.console-log-row:hover { background: var(--surface-2); }
.console-log-table td {
  padding: 10px 12px; font-size: 12px; color: var(--text);
  vertical-align: middle;
}
.console-log-empty {
  text-align: center; padding: 32px 16px;
  color: var(--text-faint); font-size: 13px;
}
.console-log-time { font-family: var(--font-mono); font-size: 11px; color: var(--text-muted); white-space: nowrap; }
.console-log-event { display: flex; align-items: center; gap: 7px; min-width: 0; }
.console-log-method {
  flex: none; font-size: 10px; font-weight: 700;
  padding: 2px 6px; border-radius: 4px;
  background: var(--surface-3); color: var(--primary);
}
.console-log-path { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); font-size: 11px; color: var(--text-soft); }
.console-log-workflow-badge {
  flex: none; font-size: 9px; font-weight: 700; padding: 2px 5px; border-radius: 4px;
  background: color-mix(in srgb, var(--info) 12%, transparent); color: var(--info);
}
.console-log-workflow-content { min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.console-log-workflow-meta { display: flex; gap: 7px; color: var(--text-faint); font-size: 10px; white-space: nowrap; overflow: hidden; }
.console-log-workflow-message { color: var(--text-soft); font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.console-log-status {
  display: inline-block; padding: 3px 8px;
  border-radius: 6px; font-size: 11px; font-weight: 600;
}
.console-log-status.status-success { background: rgba(34, 197, 94, 0.12); color: var(--success); }
.console-log-status.status-error { background: rgba(239, 68, 68, 0.12); color: var(--danger); }
.console-log-status.status-other { background: var(--surface-3); color: var(--text-muted); }
.console-log-status.status-none { background: var(--surface-3); color: var(--text-faint); }
.console-log-level {
  display: inline-block; padding: 3px 8px; border-radius: 6px;
  font-size: 10px; font-weight: 700; text-transform: uppercase;
  background: var(--surface-3); color: var(--text-muted);
}
.console-log-level.level-info { background: color-mix(in srgb, var(--info) 12%, transparent); color: var(--info); }
.console-log-level.level-warn { background: rgba(245, 158, 11, 0.12); color: #d97706; }
.console-log-level.level-error { background: rgba(239, 68, 68, 0.12); color: var(--danger); }
.console-log-row.workflow-error { background: rgba(239, 68, 68, 0.035); }

.console-log-body-cell {
  display: flex; align-items: center; gap: 8px; min-width: 0;
}
.console-log-body-toggle {
  flex: none; padding: 2px 8px; border-radius: 4px;
  font-size: 10px; font-weight: 600;
  background: var(--surface-3); color: var(--text-muted);
  border: 1px solid var(--border-soft);
  cursor: pointer; transition: all 0.15s ease;
}
.console-log-body-toggle:hover { background: var(--primary); color: #fff; border-color: var(--primary); }
.console-log-body-cell code {
  flex: 1; min-width: 0; font-family: var(--font-mono);
  font-size: 11px; color: var(--text-muted);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.console-log-no-details { color: var(--text-faint); }
.console-log-expanded { background: var(--surface-2); }
.console-log-expanded td { padding: 14px 16px; }
.console-log-expanded pre {
  margin: 0; max-height: 240px; overflow: auto;
  white-space: pre-wrap; word-break: break-word;
  font-family: var(--font-mono); font-size: 11px; line-height: 1.5;
  background: var(--surface-3); border: 1px solid var(--border);
  border-radius: var(--radius-sm); padding: 12px;
  color: var(--text-soft);
}
</style>

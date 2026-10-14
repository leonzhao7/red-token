<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { Users as UsersIcon, UserPlus, Edit, Trash2, Key, Shield, ShieldOff, LoaderCircle, Search, X, AlertTriangle } from 'lucide-vue-next'
import { toast } from '../composables/toast'
import { listUsers, createUser, updateUser, updateUserPassword, deleteUser } from '../api/users'
import type { User, CreateUserPayload, UpdateUserPayload, UpdatePasswordPayload } from '../api/users'

const loading = ref(true)
const loadError = ref('')
const users = ref<User[]>([])
const search = ref('')

const filtered = computed(() =>
  users.value.filter(u => {
    return u.username.toLowerCase().includes(search.value.toLowerCase())
  })
)

const showCreateDialog = ref(false)
const showEditDialog = ref(false)
const showPasswordDialog = ref(false)
const showDeleteDialog = ref(false)

const createForm = ref<CreateUserPayload>({
  username: '',
  password: '',
  enabled: true
})

const editForm = ref<UpdateUserPayload>({
  username: '',
  enabled: true
})

const passwordForm = ref<UpdatePasswordPayload>({
  current_password: '',
  new_password: ''
})

const editingUser = ref<User | null>(null)
const deletingUser = ref<User | null>(null)

async function loadUsers() {
  loading.value = true
  loadError.value = ''
  try {
    const resp = await listUsers()
    users.value = resp.items
  } catch (e: any) {
    loadError.value = e?.message || '加载失败'
  } finally {
    loading.value = false
  }
}

function openCreateDialog() {
  createForm.value = { username: '', password: '', enabled: true }
  showCreateDialog.value = true
}

async function handleCreate() {
  try {
    await createUser(createForm.value)
    toast('创建成功', `用户 ${createForm.value.username} 已创建`, 'success')
    showCreateDialog.value = false
    await loadUsers()
  } catch (e: any) {
    toast('创建失败', e.message, 'error')
  }
}

function openEditDialog(user: User) {
  editingUser.value = user
  editForm.value = { username: user.username, enabled: user.enabled }
  showEditDialog.value = true
}

async function handleEdit() {
  if (!editingUser.value) return
  try {
    await updateUser(editingUser.value.id, editForm.value)
    toast('更新成功', `用户 ${editForm.value.username} 已更新`, 'success')
    showEditDialog.value = false
    await loadUsers()
  } catch (e: any) {
    toast('更新失败', e.message, 'error')
  }
}

function openPasswordDialog(user: User) {
  editingUser.value = user
  passwordForm.value = { current_password: '', new_password: '' }
  showPasswordDialog.value = true
}

async function handlePasswordChange() {
  if (!editingUser.value) return
  try {
    await updateUserPassword(editingUser.value.id, passwordForm.value)
    toast('密码已更新', `用户 ${editingUser.value.username} 的密码已修改`, 'success')
    showPasswordDialog.value = false
  } catch (e: any) {
    toast('修改失败', e.message, 'error')
  }
}

function openDeleteDialog(user: User) {
  deletingUser.value = user
  showDeleteDialog.value = true
}

async function handleDelete() {
  if (!deletingUser.value) return
  try {
    await deleteUser(deletingUser.value.id)
    toast('删除成功', `用户 ${deletingUser.value.username} 已删除`, 'success')
    showDeleteDialog.value = false
    await loadUsers()
  } catch (e: any) {
    toast('删除失败', e.message, 'error')
  }
}

onMounted(loadUsers)
</script>

<template>
  <div class="page stagger">
    <section class="toolbar panel">
      <div class="search-box" style="width: 240px">
        <Search :size="15" />
        <input v-model="search" class="input" placeholder="搜索用户名…" />
        <button v-if="search" class="search-clear" aria-label="清除搜索" @click="search = ''"><X :size="14" /></button>
      </div>
      <div class="spacer"></div>
      <button class="btn btn-primary" @click="openCreateDialog">
        <UserPlus :size="15" />
        添加用户
      </button>
    </section>

    <div v-if="loading" class="load-center">
      <LoaderCircle :size="20" class="spin" /><span>正在加载…</span>
    </div>

    <div v-else-if="loadError" class="load-center err">
      <AlertTriangle :size="16" /><span>{{ loadError }}</span>
      <button class="btn btn-ghost btn-sm" @click="loadUsers">重试</button>
    </div>

    <section v-else class="users-grid">
      <article v-for="user in filtered" :key="user.id" class="user-card" :class="{ off: !user.enabled }">
        <div class="uc-glow"></div>
        <header class="uc-head">
          <div class="uc-id">
            <span class="uc-avatar">{{ user.username.charAt(0).toUpperCase() }}</span>
            <div class="uc-name">{{ user.username }}</div>
          </div>
          <span class="pill" :class="user.enabled ? 'success' : 'neutral'">
            <span class="dot" />{{ user.enabled ? '已启用' : '已禁用' }}
          </span>
        </header>

        <div class="uc-badge-row">
          <span v-if="user.is_default_password" class="pill warning">
            <Key :size="10" /> 默认密码
          </span>
        </div>

        <div class="uc-rows">
          <div class="uc-row">
            <span class="uc-row-label">最后登录</span>
            <span class="uc-row-val">{{ user.last_login_at || '从未登录' }}</span>
          </div>
          <div class="uc-row">
            <span class="uc-row-label">创建时间</span>
            <span class="uc-row-val">{{ user.created_at }}</span>
          </div>
        </div>

        <footer class="uc-foot">
          <div class="spacer"></div>
          <button class="icon-btn primary" title="编辑" @click="openEditDialog(user)"><Edit :size="14" /></button>
          <button class="icon-btn" title="修改密码" @click="openPasswordDialog(user)"><Key :size="14" /></button>
          <button class="icon-btn danger" title="删除" @click="openDeleteDialog(user)"><Trash2 :size="14" /></button>
        </footer>
      </article>
    </section>

    <!-- Create User Dialog -->
    <div v-if="showCreateDialog" class="dialog-overlay" @click.self="showCreateDialog = false">
      <div class="dialog">
        <div class="dialog-header">
          <h2>添加用户</h2>
          <button class="btn btn-ghost btn-sm" @click="showCreateDialog = false">×</button>
        </div>
        <form @submit.prevent="handleCreate" class="dialog-body">
          <div class="field">
            <label class="field-label">用户名</label>
            <input v-model="createForm.username" type="text" placeholder="输入用户名" required />
          </div>
          <div class="field">
            <label class="field-label">密码</label>
            <input v-model="createForm.password" type="password" placeholder="至少 4 个字符" required />
          </div>
          <div class="field-row">
            <label class="toggle">
              <input v-model="createForm.enabled" type="checkbox" />
              <span class="toggle-slider"></span>
              <span class="toggle-label">启用账号</span>
            </label>
          </div>
          <div class="dialog-footer">
            <button type="button" class="btn btn-ghost" @click="showCreateDialog = false">取消</button>
            <button type="submit" class="btn btn-primary">创建</button>
          </div>
        </form>
      </div>
    </div>

    <!-- Edit User Dialog -->
    <div v-if="showEditDialog" class="dialog-overlay" @click.self="showEditDialog = false">
      <div class="dialog">
        <div class="dialog-header">
          <h2>编辑用户</h2>
          <button class="btn btn-ghost btn-sm" @click="showEditDialog = false">×</button>
        </div>
        <form @submit.prevent="handleEdit" class="dialog-body">
          <div class="field">
            <label class="field-label">用户名</label>
            <input v-model="editForm.username" type="text" required />
          </div>
          <div class="field-row">
            <label class="toggle">
              <input v-model="editForm.enabled" type="checkbox" />
              <span class="toggle-slider"></span>
              <span class="toggle-label">启用账号</span>
            </label>
          </div>
          <div class="dialog-footer">
            <button type="button" class="btn btn-ghost" @click="showEditDialog = false">取消</button>
            <button type="submit" class="btn btn-primary">保存</button>
          </div>
        </form>
      </div>
    </div>

    <!-- Change Password Dialog -->
    <div v-if="showPasswordDialog" class="dialog-overlay" @click.self="showPasswordDialog = false">
      <div class="dialog">
        <div class="dialog-header">
          <h2>修改密码</h2>
          <button class="btn btn-ghost btn-sm" @click="showPasswordDialog = false">×</button>
        </div>
        <form @submit.prevent="handlePasswordChange" class="dialog-body">
          <div class="field">
            <label class="field-label">当前密码</label>
            <input v-model="passwordForm.current_password" type="password" placeholder="••••••••" required />
          </div>
          <div class="field">
            <label class="field-label">新密码</label>
            <input v-model="passwordForm.new_password" type="password" placeholder="至少 4 个字符" required />
          </div>
          <div class="dialog-footer">
            <button type="button" class="btn btn-ghost" @click="showPasswordDialog = false">取消</button>
            <button type="submit" class="btn btn-primary">确认修改</button>
          </div>
        </form>
      </div>
    </div>

    <!-- Delete Confirmation Dialog -->
    <div v-if="showDeleteDialog" class="dialog-overlay" @click.self="showDeleteDialog = false">
      <div class="dialog dialog-sm">
        <div class="dialog-header">
          <h2>删除用户</h2>
          <button class="btn btn-ghost btn-sm" @click="showDeleteDialog = false">×</button>
        </div>
        <div class="dialog-body">
          <p>确定要删除用户 <strong>{{ deletingUser?.username }}</strong> 吗？此操作无法撤销。</p>
          <div class="dialog-footer">
            <button class="btn btn-ghost" @click="showDeleteDialog = false">取消</button>
            <button class="btn btn-danger" @click="handleDelete">删除</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.users-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
  gap: 16px;
}

.user-card {
  position: relative;
  background: var(--glass);
  border: 1px solid var(--border);
  border-radius: 16px;
  padding: 20px;
  transition: all 0.2s var(--ease-out);
  overflow: hidden;
}

.user-card:hover {
  border-color: var(--border-strong);
  box-shadow: var(--shadow-lg);
}

.user-card.off {
  opacity: 0.6;
}

.uc-glow {
  position: absolute;
  top: -50%;
  left: -50%;
  width: 200%;
  height: 200%;
  background: radial-gradient(circle at center, var(--primary-dim) 0%, transparent 70%);
  opacity: 0;
  transition: opacity 0.3s var(--ease-out);
  pointer-events: none;
}

.user-card:hover .uc-glow {
  opacity: 0.15;
}

.uc-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.uc-id {
  display: flex;
  align-items: center;
  gap: 12px;
}

.uc-avatar {
  width: 40px;
  height: 40px;
  border-radius: 999px;
  background: var(--primary-dim);
  color: var(--primary);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
  font-weight: 600;
  flex-shrink: 0;
}

.uc-name {
  font-size: 16px;
  font-weight: 600;
  color: var(--text);
}

.uc-badge-row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 14px;
  min-height: 20px;
}

.uc-rows {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 14px;
  background: var(--surface);
  border-radius: 12px;
  margin-bottom: 14px;
}

.uc-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 13px;
  gap: 12px;
}

.uc-row-label {
  color: var(--text-faint);
  flex-shrink: 0;
}

.uc-row-val {
  font-weight: 500;
  color: var(--text);
  text-align: right;
  overflow: hidden;
  text-overflow: ellipsis;
}

.uc-foot {
  display: flex;
  gap: 8px;
  align-items: center;
}

.dialog-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.6);
  backdrop-filter: blur(8px);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  animation: fadeIn 0.2s var(--ease-out);
}

.dialog {
  background: var(--glass-strong);
  border: 1px solid var(--border-strong);
  border-radius: 20px;
  width: 90%;
  max-width: 480px;
  max-height: 90vh;
  overflow: auto;
  box-shadow: var(--shadow-2xl);
  animation: slideUp 0.3s var(--ease-out);
}

.dialog-sm {
  max-width: 400px;
}

.dialog-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 24px 28px;
  border-bottom: 1px solid var(--border);
}

.dialog-header h2 {
  font-size: 20px;
  font-weight: 600;
}

.dialog-body {
  padding: 28px;
}

.dialog-body p {
  line-height: 1.6;
  color: var(--text-faint);
}

.field {
  margin-bottom: 20px;
}

.field-label {
  display: block;
  font-size: 14px;
  font-weight: 500;
  margin-bottom: 8px;
  color: var(--text);
}

.field input[type="text"],
.field input[type="password"] {
  width: 100%;
  padding: 12px 16px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 12px;
  color: var(--text);
  font-size: 15px;
  transition: all 0.2s var(--ease-out);
}

.field input:focus {
  border-color: var(--primary);
  outline: none;
}

.field-row {
  margin-bottom: 20px;
}

.toggle {
  display: flex;
  align-items: center;
  gap: 12px;
  cursor: pointer;
}

.toggle input[type="checkbox"] {
  display: none;
}

.toggle-slider {
  width: 44px;
  height: 24px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 999px;
  position: relative;
  transition: all 0.2s var(--ease-out);
}

.toggle-slider::after {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 18px;
  height: 18px;
  background: var(--text-faint);
  border-radius: 999px;
  transition: all 0.2s var(--ease-out);
}

.toggle input:checked + .toggle-slider {
  background: var(--primary-dim);
  border-color: var(--primary);
}

.toggle input:checked + .toggle-slider::after {
  left: 22px;
  background: var(--primary);
}

.toggle-label {
  font-size: 14px;
  font-weight: 500;
}

.dialog-footer {
  display: flex;
  gap: 12px;
  justify-content: flex-end;
  margin-top: 24px;
  padding-top: 20px;
  border-top: 1px solid var(--border);
}

@keyframes fadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
}

@keyframes slideUp {
  from {
    opacity: 0;
    transform: translateY(20px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}
</style>

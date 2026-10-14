<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Lock, User } from 'lucide-vue-next'
import { useAuth } from '../composables/useAuth'
import { toast } from '../composables/toast'

const username = ref('admin')
const password = ref('admin')
const submitting = ref(false)
const router = useRouter()
const auth = useAuth()

async function handleLogin() {
  if (submitting.value) return
  submitting.value = true

  const error = await auth.login(username.value, password.value)

  if (error) {
    toast('登录失败', error, 'error')
    submitting.value = false
  } else {
    const redirect = router.currentRoute.value.query.redirect as string
    router.push(redirect || '/')
  }
}

onMounted(() => {
  // Clear any stale state
  username.value = 'admin'
  password.value = 'admin'
})
</script>

<template>
  <div class="login-page">
    <div class="login-card">
      <div class="login-header">
        <div class="login-icon">
          <Lock :size="48" />
        </div>
        <h1>RED TOKEN</h1>
        <p class="login-subtitle">管理后台登录</p>
      </div>

      <form @submit.prevent="handleLogin" class="login-form">
        <div class="field">
          <label class="field-label">
            <User :size="16" /> 用户名
          </label>
          <input
            v-model="username"
            type="text"
            placeholder="admin"
            required
            :disabled="submitting"
            autocomplete="username"
          />
        </div>

        <div class="field">
          <label class="field-label">
            <Lock :size="16" /> 密码
          </label>
          <input
            v-model="password"
            type="password"
            placeholder="••••••••"
            required
            :disabled="submitting"
            autocomplete="current-password"
          />
        </div>

        <button type="submit" class="btn btn-primary w-full" :disabled="submitting">
          {{ submitting ? '登录中...' : '登录' }}
        </button>
      </form>

      <p class="login-hint">默认账号 admin/admin，首次登录后请立即修改密码</p>
    </div>
  </div>
</template>

<style scoped>
.login-page {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  background: linear-gradient(135deg, #0A0D12 0%, #0F131C 50%, #161D2B 100%);
  position: relative;
  overflow: hidden;
}

.login-page::before {
  content: '';
  position: absolute;
  top: -50%;
  left: -50%;
  width: 200%;
  height: 200%;
  background: radial-gradient(circle at 30% 50%, rgba(56, 189, 248, 0.08) 0%, transparent 50%),
              radial-gradient(circle at 70% 50%, rgba(110, 231, 183, 0.06) 0%, transparent 50%);
  animation: drift 20s ease-in-out infinite;
}

@keyframes drift {
  0%, 100% { transform: translate(0, 0) rotate(0deg); }
  50% { transform: translate(2%, 2%) rotate(1deg); }
}

.login-card {
  width: 100%;
  max-width: 420px;
  padding: 48px 32px;
  background: rgba(15, 19, 28, 0.85);
  backdrop-filter: blur(20px);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 24px;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.4),
              0 0 1px rgba(255, 255, 255, 0.1) inset;
  position: relative;
  z-index: 1;
}

.login-header {
  text-align: center;
  margin-bottom: 32px;
}

.login-icon {
  display: inline-flex;
  padding: 16px;
  background: rgba(56, 189, 248, 0.12);
  border-radius: 50%;
  color: #38BDF8;
  margin-bottom: 16px;
}

.login-header h1 {
  font-size: 32px;
  font-weight: 700;
  letter-spacing: -0.02em;
  background: linear-gradient(135deg, #FFFFFF 0%, rgba(255, 255, 255, 0.7) 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}

.login-subtitle {
  color: rgba(255, 255, 255, 0.5);
  margin-top: 8px;
  font-size: 15px;
}

.login-form {
  margin-bottom: 24px;
}

.field {
  margin-bottom: 20px;
}

.field-label {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 500;
  margin-bottom: 8px;
  color: rgba(255, 255, 255, 0.9);
}

.field input {
  width: 100%;
  padding: 14px 16px;
  background: rgba(22, 29, 43, 0.6);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 12px;
  color: #FFFFFF;
  font-size: 15px;
  transition: all 0.2s ease;
}

.field input:focus {
  outline: none;
  border-color: #38BDF8;
  background: rgba(22, 29, 43, 0.8);
  box-shadow: 0 0 0 3px rgba(56, 189, 248, 0.1);
}

.field input:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.field input::placeholder {
  color: rgba(255, 255, 255, 0.3);
}

.btn {
  padding: 14px 24px;
  border-radius: 12px;
  font-size: 15px;
  font-weight: 600;
  border: none;
  cursor: pointer;
  transition: all 0.2s ease;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}

.btn-primary {
  background: linear-gradient(135deg, #38BDF8 0%, #2D9FD9 100%);
  color: #FFFFFF;
  box-shadow: 0 4px 12px rgba(56, 189, 248, 0.3);
}

.btn-primary:hover:not(:disabled) {
  box-shadow: 0 6px 20px rgba(56, 189, 248, 0.4);
  transform: translateY(-1px);
}

.btn-primary:disabled {
  opacity: 0.6;
  cursor: not-allowed;
  transform: none;
}

.w-full {
  width: 100%;
}

.login-hint {
  text-align: center;
  font-size: 13px;
  color: rgba(255, 255, 255, 0.4);
  line-height: 1.6;
}
</style>

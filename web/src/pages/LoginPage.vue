<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { NInput, NButton, NIcon } from 'naive-ui'
import { PersonOutline, LockClosedOutline, ShieldCheckmarkOutline } from '@vicons/ionicons5'
import { useAuth } from '../composables/useAuth'
import {
  login as apiLogin,
  getStartupConfig,
  createStartupUser,
  completeStartup,
} from '../api/client'

const { login } = useAuth()
const router = useRouter()

const username = ref('')
const password = ref('')
const error = ref('')
const loading = ref(false)
const isSetup = ref(false)
const visible = ref(false)
const shake = ref(false)

const subtitle = computed(() =>
  isSetup.value ? '首次启动，请创建管理员账户' : '登录到您的媒体服务器'
)
const btnText = computed(() =>
  isSetup.value ? '创建管理员' : '登  录'
)

function triggerShake() {
  shake.value = true
  setTimeout(() => { shake.value = false }, 500)
}

onMounted(() => {
  getStartupConfig()
    .then((config) => {
      if (!config.IsComplete) isSetup.value = true
    })
    .catch(() => {})
  requestAnimationFrame(() => { visible.value = true })
  initParticles()
})

/* ── Floating particle background ── */
const canvasRef = ref<HTMLCanvasElement | null>(null)
let animId = 0

interface Particle {
  x: number; y: number; r: number
  vx: number; vy: number; alpha: number
}

function initParticles() {
  const canvas = canvasRef.value
  if (!canvas) return
  const ctx = canvas.getContext('2d')
  if (!ctx) return

  let w = canvas.width = window.innerWidth
  let h = canvas.height = window.innerHeight

  const count = Math.min(60, Math.floor((w * h) / 18000))
  const particles: Particle[] = Array.from({ length: count }, () => ({
    x: Math.random() * w,
    y: Math.random() * h,
    r: Math.random() * 1.5 + 0.5,
    vx: (Math.random() - 0.5) * 0.3,
    vy: (Math.random() - 0.5) * 0.3,
    alpha: Math.random() * 0.4 + 0.1,
  }))

  function resize() {
    w = canvas!.width = window.innerWidth
    h = canvas!.height = window.innerHeight
  }
  window.addEventListener('resize', resize)

  function draw() {
    ctx!.clearRect(0, 0, w, h)
    for (const p of particles) {
      p.x += p.vx
      p.y += p.vy
      if (p.x < 0) p.x = w
      if (p.x > w) p.x = 0
      if (p.y < 0) p.y = h
      if (p.y > h) p.y = 0
      ctx!.beginPath()
      ctx!.arc(p.x, p.y, p.r, 0, Math.PI * 2)
      ctx!.fillStyle = `rgba(0, 164, 220, ${p.alpha})`
      ctx!.fill()
    }

    for (let i = 0; i < particles.length; i++) {
      for (let j = i + 1; j < particles.length; j++) {
        const dx = particles[i].x - particles[j].x
        const dy = particles[i].y - particles[j].y
        const dist = Math.sqrt(dx * dx + dy * dy)
        if (dist < 120) {
          ctx!.beginPath()
          ctx!.moveTo(particles[i].x, particles[i].y)
          ctx!.lineTo(particles[j].x, particles[j].y)
          ctx!.strokeStyle = `rgba(0, 164, 220, ${0.06 * (1 - dist / 120)})`
          ctx!.lineWidth = 0.5
          ctx!.stroke()
        }
      }
    }

    animId = requestAnimationFrame(draw)
  }
  draw()
}

onUnmounted(() => { cancelAnimationFrame(animId) })

async function handleLogin() {
  error.value = ''
  loading.value = true
  if (!username.value) {
    error.value = '请输入用户名'
    loading.value = false
    triggerShake()
    return
  }
  try {
    const result = await apiLogin(username.value, password.value)
    login(result.User.Id, result.User.Name, result.AccessToken, result.User.Policy?.IsAdministrator || false)
    router.push('/')
  } catch {
    error.value = '用户名或密码错误'
    triggerShake()
  } finally {
    loading.value = false
  }
}

async function handleSetup() {
  if (!username.value) {
    error.value = '用户名不能为空'
    triggerShake()
    return
  }
  loading.value = true
  try {
    await createStartupUser(username.value, password.value)
    await completeStartup()
    const result = await apiLogin(username.value, password.value)
    login(result.User.Id, result.User.Name, result.AccessToken, true)
    router.push({ name: 'admin_overview' })
  } catch {
    error.value = '设置失败'
    triggerShake()
  } finally {
    loading.value = false
  }
}

async function onSubmit() {
  if (isSetup.value) await handleSetup()
  else await handleLogin()
}
</script>

<template>
  <div class="login-page">
    <canvas ref="canvasRef" class="login-particles" />

    <div class="login-glow login-glow--primary" />
    <div class="login-glow login-glow--secondary" />

    <div
      class="login-card"
      :class="{ 'login-card--visible': visible, 'login-card--shake': shake }"
    >
      <!-- Logo -->
      <div class="login-logo">
        <div class="login-logo__icon">
          <svg viewBox="0 0 40 40" fill="none" xmlns="http://www.w3.org/2000/svg">
            <rect width="40" height="40" rx="10" fill="url(#logo-grad)" />
            <path d="M12 13h16v2H14v4h12v2H14v6h-2V13z" fill="#fff" fill-opacity="0.95" />
            <defs>
              <linearGradient id="logo-grad" x1="0" y1="0" x2="40" y2="40">
                <stop stop-color="#00a4dc" />
                <stop offset="1" stop-color="#0077b6" />
              </linearGradient>
            </defs>
          </svg>
        </div>
        <span class="login-logo__text">FYMS</span>
      </div>

      <!-- Badge for setup mode -->
      <transition name="scale-fade">
        <div v-if="isSetup" class="login-badge">
          <n-icon :component="ShieldCheckmarkOutline" :size="14" />
          <span>初始化设置</span>
        </div>
      </transition>

      <p class="login-subtitle">{{ subtitle }}</p>

      <!-- Error -->
      <transition name="fade-slide">
        <div v-if="error" class="login-error">
          <span class="login-error__dot" />
          {{ error }}
        </div>
      </transition>

      <!-- Form -->
      <form class="login-form" @submit.prevent="onSubmit">
        <div class="login-field">
          <label class="login-field__label">用户名</label>
          <n-input
            v-model:value="username"
            placeholder="请输入用户名"
            size="large"
            :input-props="{ autocomplete: 'username' }"
            @update:value="() => { error = '' }"
          >
            <template #prefix>
              <n-icon :component="PersonOutline" :size="18" style="color: var(--c-slate-400)" />
            </template>
          </n-input>
        </div>

        <div class="login-field">
          <label class="login-field__label">密码</label>
          <n-input
            v-model:value="password"
            type="password"
            show-password-on="click"
            placeholder="请输入密码"
            size="large"
            :input-props="{ autocomplete: isSetup ? 'new-password' : 'current-password' }"
            @update:value="() => { error = '' }"
          >
            <template #prefix>
              <n-icon :component="LockClosedOutline" :size="18" style="color: var(--c-slate-400)" />
            </template>
          </n-input>
        </div>

        <n-button
          class="login-submit"
          type="primary"
          attr-type="submit"
          block
          size="large"
          :loading="loading"
        >
          {{ btnText }}
        </n-button>
      </form>

      <p class="login-footer">
        {{ isSetup ? '此账户将拥有完整的管理员权限' : '安全连接到您的 FYMS 实例' }}
      </p>
    </div>
  </div>
</template>

<style scoped>
.login-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(160deg, #06060b 0%, #0c0c18 40%, #10101f 100%);
  position: relative;
  overflow: hidden;
}

.login-particles {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  pointer-events: none;
  z-index: 0;
}

/* Ambient glow orbs */
.login-glow {
  position: absolute;
  border-radius: 50%;
  pointer-events: none;
  filter: blur(80px);
}

.login-glow--primary {
  width: 500px;
  height: 500px;
  top: 30%;
  left: 50%;
  transform: translate(-50%, -50%);
  background: radial-gradient(circle, rgba(0, 164, 220, 0.08) 0%, transparent 70%);
  animation: glowPulse 8s ease-in-out infinite;
}

.login-glow--secondary {
  width: 350px;
  height: 350px;
  bottom: 10%;
  right: 15%;
  background: radial-gradient(circle, rgba(0, 119, 182, 0.05) 0%, transparent 70%);
  animation: glowPulse 10s ease-in-out infinite 3s;
}

@keyframes glowPulse {
  0%, 100% { opacity: 0.6; transform: translate(-50%, -50%) scale(1); }
  50% { opacity: 1; transform: translate(-50%, -50%) scale(1.15); }
}

/* Card */
.login-card {
  width: 100%;
  max-width: 400px;
  margin: 0 20px;
  padding: 44px 40px 36px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.06);
  border-radius: 20px;
  backdrop-filter: blur(24px) saturate(1.3);
  -webkit-backdrop-filter: blur(24px) saturate(1.3);
  box-shadow:
    0 0 0 1px rgba(255, 255, 255, 0.03) inset,
    0 24px 80px rgba(0, 0, 0, 0.5),
    0 2px 16px rgba(0, 0, 0, 0.3);
  position: relative;
  z-index: 1;
  opacity: 0;
  transform: translateY(20px) scale(0.98);
  transition: opacity 0.6s cubic-bezier(0.4, 0, 0.2, 1),
              transform 0.6s cubic-bezier(0.4, 0, 0.2, 1);
}

.login-card--visible {
  opacity: 1;
  transform: translateY(0) scale(1);
}

.login-card--shake {
  animation: cardShake 0.5s cubic-bezier(0.36, 0.07, 0.19, 0.97);
}

@keyframes cardShake {
  0%, 100% { transform: translateX(0); }
  10%, 50%, 90% { transform: translateX(-4px); }
  30%, 70% { transform: translateX(4px); }
}

/* Logo */
.login-logo {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  margin-bottom: 8px;
}

.login-logo__icon {
  width: 40px;
  height: 40px;
  flex-shrink: 0;
}

.login-logo__icon svg {
  width: 100%;
  height: 100%;
  filter: drop-shadow(0 2px 8px rgba(0, 164, 220, 0.3));
}

.login-logo__text {
  font-size: 26px;
  font-weight: 800;
  letter-spacing: 4px;
  background: linear-gradient(135deg, #00a4dc 0%, #60cfff 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}

/* Setup badge */
.login-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin: 12px auto 0;
  padding: 4px 14px;
  font-size: 12px;
  font-weight: 600;
  color: #00a4dc;
  background: rgba(0, 164, 220, 0.1);
  border: 1px solid rgba(0, 164, 220, 0.2);
  border-radius: 20px;
  width: fit-content;
  text-align: center;
}

/* Subtitle */
.login-subtitle {
  text-align: center;
  color: var(--c-slate-400);
  font-size: 14px;
  margin: 12px 0 28px;
  line-height: 1.5;
}

/* Error */
.login-error {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 10px 16px;
  margin-bottom: 20px;
  font-size: 13px;
  color: #f87171;
  background: rgba(248, 113, 113, 0.08);
  border: 1px solid rgba(248, 113, 113, 0.15);
  border-radius: 10px;
}

.login-error__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #f87171;
  flex-shrink: 0;
  animation: pulse 2s ease-in-out infinite;
}

/* Form */
.login-form {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.login-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.login-field__label {
  font-size: 13px;
  font-weight: 500;
  color: var(--c-slate-300);
  padding-left: 2px;
}

/* Input overrides */
.login-form :deep(.n-input) {
  --n-border-radius: 12px !important;
  --n-height-large: 48px !important;
  --n-color: rgba(255, 255, 255, 0.06) !important;
  --n-color-focus: rgba(255, 255, 255, 0.08) !important;
  --n-color-disabled: rgba(255, 255, 255, 0.04) !important;
  --n-text-color: rgba(255, 255, 255, 0.96) !important;
  --n-placeholder-color: rgba(203, 213, 225, 0.66) !important;
  --n-caret-color: var(--app-primary) !important;
  --n-icon-color: rgba(203, 213, 225, 0.72) !important;
  --n-icon-color-hover: rgba(255, 255, 255, 0.92) !important;
  --n-icon-color-pressed: rgba(255, 255, 255, 0.92) !important;
  --n-clear-color: rgba(203, 213, 225, 0.72) !important;
  --n-clear-color-hover: rgba(255, 255, 255, 0.92) !important;
  --n-border: 1px solid rgba(255, 255, 255, 0.08) !important;
  --n-border-hover: 1px solid rgba(0, 164, 220, 0.26) !important;
  --n-border-focus: 1px solid rgba(0, 164, 220, 0.5) !important;
  --n-box-shadow-focus: 0 0 0 3px rgba(0, 164, 220, 0.08) !important;
  overflow: hidden;
  border-radius: 12px;
}

.login-form :deep(.n-input),
.login-form :deep(.n-input .n-input-wrapper),
.login-form :deep(.n-input .n-input__border),
.login-form :deep(.n-input .n-input__state-border) {
  border-radius: 12px !important;
  border-color: rgba(255, 255, 255, 0.08) !important;
  transition: border-color 0.25s ease, box-shadow 0.25s ease;
}

.login-form :deep(.n-input .n-input-wrapper) {
  background: linear-gradient(180deg, rgba(255, 255, 255, 0.08), rgba(255, 255, 255, 0.05));
  box-shadow:
    0 1px 0 rgba(255, 255, 255, 0.04) inset,
    0 10px 24px rgba(0, 0, 0, 0.18);
  transition: background 0.25s ease, box-shadow 0.25s ease;
}

.login-form :deep(.n-input:hover .n-input__state-border) {
  border-color: rgba(0, 164, 220, 0.3) !important;
}

.login-form :deep(.n-input.n-input--focus .n-input__state-border) {
  border-color: rgba(0, 164, 220, 0.5) !important;
  box-shadow: 0 0 0 3px rgba(0, 164, 220, 0.08) !important;
}

.login-form :deep(.n-input.n-input--focus .n-input-wrapper) {
  background: linear-gradient(180deg, rgba(255, 255, 255, 0.1), rgba(255, 255, 255, 0.07));
  box-shadow:
    0 1px 0 rgba(255, 255, 255, 0.05) inset,
    0 14px 30px rgba(0, 0, 0, 0.22);
}

.login-form :deep(.n-input .n-input-wrapper) {
  padding-left: 14px;
}

.login-form :deep(.n-input .n-input__suffix) {
  background: transparent !important;
  border-radius: 0 12px 12px 0;
}

/* Submit button */
.login-submit {
  margin-top: 6px;
  --n-height-large: 48px !important;
  --n-border-radius: 12px !important;
  font-weight: 600 !important;
  font-size: 15px !important;
  letter-spacing: 1px;
  transition: transform 0.2s ease, box-shadow 0.2s ease !important;
}

.login-submit:not(:disabled):hover {
  transform: translateY(-1px);
  box-shadow: 0 4px 20px rgba(0, 164, 220, 0.35);
}

.login-submit:not(:disabled):active {
  transform: translateY(0);
}

/* Footer */
.login-footer {
  text-align: center;
  color: var(--c-slate-500);
  font-size: 12px;
  margin: 24px 0 0;
  line-height: 1.5;
}

/* Responsive */
@media (max-width: 480px) {
  .login-card {
    margin: 0 16px;
    padding: 36px 24px 28px;
  }
}
</style>

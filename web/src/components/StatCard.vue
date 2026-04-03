<script setup lang="ts">
import { NIcon } from 'naive-ui'
import type { Component } from 'vue'

defineProps<{
  title: string
  value: string | number
  subTitle?: string
  icon: Component
  type?: 'primary' | 'success' | 'warning' | 'error' | 'info'
  valueSize?: 'lg' | 'md' | 'sm'
}>()
</script>

<template>
  <div class="stat-card" :class="type || 'primary'">
    <div class="card-body">
      <div class="header">
        <span class="title">{{ title }}</span>
        <div class="icon-box">
          <n-icon :component="icon" />
        </div>
      </div>
      <div class="value" :class="valueSize || 'lg'">{{ value }}</div>
      <div class="footer" v-if="subTitle">{{ subTitle }}</div>
    </div>
    <!-- Decorative background icon -->
    <div class="bg-icon">
      <n-icon :component="icon" />
    </div>
  </div>
</template>

<style scoped>
.stat-card {
  position: relative;
  background: var(--app-surface-2);
  border: 1px solid var(--app-border);
  border-radius: var(--app-radius-card);
  overflow: hidden;
  transition: transform 0.3s cubic-bezier(0.4, 0, 0.2, 1),
              box-shadow 0.3s cubic-bezier(0.4, 0, 0.2, 1),
              border-color 0.3s ease;
  box-shadow: var(--app-shadow-0);
  backdrop-filter: blur(var(--app-glass-blur));
  -webkit-backdrop-filter: blur(var(--app-glass-blur));
}

.stat-card::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 3px;
  background: linear-gradient(90deg, transparent, currentColor, transparent);
  opacity: 0;
  transition: opacity 0.3s ease;
  z-index: 3;
}

.stat-card:hover {
  transform: translateY(-4px) scale(1.02);
  box-shadow: 0 20px 25px -5px rgba(0, 0, 0, 0.1),
              0 10px 10px -5px rgba(0, 0, 0, 0.04);
}

.stat-card:hover::before {
  opacity: 0.6;
}

.card-body {
  padding: 20px 24px;
  position: relative;
  z-index: 2;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}

.title {
  font-size: 14px;
  font-weight: 500;
  color: var(--app-text-muted);
  transition: color 0.3s ease;
}

.stat-card:hover .title {
  color: var(--app-text);
}

.icon-box {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  border-radius: 10px;
  background: var(--c-slate-100);
  color: var(--c-slate-500);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  position: relative;
}

.icon-box::before {
  content: '';
  position: absolute;
  inset: -4px;
  border-radius: 12px;
  background: currentColor;
  opacity: 0;
  filter: blur(12px);
  transition: opacity 0.3s ease;
}

.stat-card:hover .icon-box {
  transform: scale(1.1) rotate(5deg);
}

.stat-card:hover .icon-box::before {
  opacity: 0.3;
}

.app-dark .icon-box {
  background: var(--c-slate-800);
  color: var(--c-slate-400);
}

.value {
  font-weight: 700;
  line-height: 1.2;
  color: var(--app-text);
  letter-spacing: -0.02em;
  margin-bottom: 4px;
  transition: transform 0.3s ease;
  word-break: break-word;
}

.value.lg {
  font-size: 32px;
}
.value.md {
  font-size: 20px;
  line-height: 1.35;
}
.value.sm {
  font-size: 16px;
  line-height: 1.4;
}

.stat-card:hover .value {
  transform: scale(1.05);
}

.footer {
  font-size: 12px;
  color: var(--app-text-muted);
  opacity: 0.8;
  transition: opacity 0.3s ease;
}

.stat-card:hover .footer {
  opacity: 1;
}

/* Background Decoration */
.bg-icon {
  position: absolute;
  right: -10px;
  bottom: -10px;
  font-size: 120px;
  opacity: 0.04;
  color: currentColor;
  z-index: 1;
  pointer-events: none;
  transform: rotate(-15deg);
  transition: all 0.4s cubic-bezier(0.4, 0, 0.2, 1);
}

.stat-card:hover .bg-icon {
  opacity: 0.08;
  transform: rotate(-10deg) scale(1.1);
}

/* Variants with gradient backgrounds */
.stat-card.primary {
  background: linear-gradient(135deg,
    rgba(var(--app-primary-rgb), 0.03) 0%,
    var(--app-surface-2) 100%);
  border-color: rgba(var(--app-primary-rgb), 0.2);
}
.stat-card.primary .icon-box {
  color: var(--app-primary);
  background: linear-gradient(135deg, rgba(var(--app-primary-rgb), 0.15), rgba(var(--app-primary-rgb), 0.08));
  box-shadow: 0 4px 12px rgba(var(--app-primary-rgb), 0.2);
}
.stat-card.primary .bg-icon { color: var(--app-primary); }
.stat-card.primary:hover {
  border-color: rgba(var(--app-primary-rgb), 0.4);
  box-shadow: 0 20px 25px -5px rgba(var(--app-primary-rgb), 0.15),
              0 10px 10px -5px rgba(var(--app-primary-rgb), 0.08);
}

.stat-card.success {
  background: linear-gradient(135deg,
    rgba(16, 185, 129, 0.03) 0%,
    var(--app-surface-2) 100%);
  border-color: rgba(16, 185, 129, 0.2);
}
.stat-card.success .icon-box {
  color: var(--app-success);
  background: linear-gradient(135deg, rgba(16, 185, 129, 0.15), rgba(16, 185, 129, 0.08));
  box-shadow: 0 4px 12px rgba(16, 185, 129, 0.2);
}
.stat-card.success .bg-icon { color: var(--app-success); }
.stat-card.success:hover {
  border-color: rgba(16, 185, 129, 0.4);
  box-shadow: 0 20px 25px -5px rgba(16, 185, 129, 0.15),
              0 10px 10px -5px rgba(16, 185, 129, 0.08);
}

.stat-card.info {
  background: linear-gradient(135deg,
    rgba(59, 130, 246, 0.03) 0%,
    var(--app-surface-2) 100%);
  border-color: rgba(59, 130, 246, 0.2);
}
.stat-card.info .icon-box {
  color: var(--app-info);
  background: linear-gradient(135deg, rgba(59, 130, 246, 0.15), rgba(59, 130, 246, 0.08));
  box-shadow: 0 4px 12px rgba(59, 130, 246, 0.2);
}
.stat-card.info .bg-icon { color: var(--app-info); }
.stat-card.info:hover {
  border-color: rgba(59, 130, 246, 0.4);
  box-shadow: 0 20px 25px -5px rgba(59, 130, 246, 0.15),
              0 10px 10px -5px rgba(59, 130, 246, 0.08);
}

.stat-card.warning {
  background: linear-gradient(135deg,
    rgba(245, 158, 11, 0.03) 0%,
    var(--app-surface-2) 100%);
  border-color: rgba(245, 158, 11, 0.2);
}
.stat-card.warning .icon-box {
  color: var(--app-warning);
  background: linear-gradient(135deg, rgba(245, 158, 11, 0.15), rgba(245, 158, 11, 0.08));
  box-shadow: 0 4px 12px rgba(245, 158, 11, 0.2);
}
.stat-card.warning .bg-icon { color: var(--app-warning); }
.stat-card.warning:hover {
  border-color: rgba(245, 158, 11, 0.4);
  box-shadow: 0 20px 25px -5px rgba(245, 158, 11, 0.15),
              0 10px 10px -5px rgba(245, 158, 11, 0.08);
}

.stat-card.error {
  background: linear-gradient(135deg,
    rgba(239, 68, 68, 0.03) 0%,
    var(--app-surface-2) 100%);
  border-color: rgba(239, 68, 68, 0.2);
}
.stat-card.error .icon-box {
  color: var(--app-error);
  background: linear-gradient(135deg, rgba(239, 68, 68, 0.15), rgba(239, 68, 68, 0.08));
  box-shadow: 0 4px 12px rgba(239, 68, 68, 0.2);
}
.stat-card.error .bg-icon { color: var(--app-error); }
.stat-card.error:hover {
  border-color: rgba(239, 68, 68, 0.4);
  box-shadow: 0 20px 25px -5px rgba(239, 68, 68, 0.15),
              0 10px 10px -5px rgba(239, 68, 68, 0.08);
}
</style>

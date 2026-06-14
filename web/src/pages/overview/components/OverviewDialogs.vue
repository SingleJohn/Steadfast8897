<script setup lang="ts">
import { NModal } from 'naive-ui'

defineProps<{
  showRestart: boolean
  showShutdown: boolean
  showUpdateConfirm: boolean
  showRollbackConfirm: boolean
  updateConfirmText: string
  rollbackConfirmText: string
}>()

const emit = defineEmits<{
  'update:showRestart': [value: boolean]
  'update:showShutdown': [value: boolean]
  'update:showUpdateConfirm': [value: boolean]
  'update:showRollbackConfirm': [value: boolean]
  restart: []
  shutdown: []
  applyUpdate: []
  rollbackUpdate: []
}>()
</script>

<template>
  <n-modal
    :show="showRestart"
    preset="dialog"
    title="重启服务器"
    type="warning"
    positive-text="确认重启"
    negative-text="取消"
    @update:show="emit('update:showRestart', $event)"
    @positive-click="emit('restart')"
    @negative-click="emit('update:showRestart', false)"
  >
    确定要重启服务器吗？所有活动连接将被断开。
  </n-modal>
  <n-modal
    :show="showShutdown"
    preset="dialog"
    title="关闭服务器"
    type="error"
    positive-text="确认关闭"
    negative-text="取消"
    @update:show="emit('update:showShutdown', $event)"
    @positive-click="emit('shutdown')"
    @negative-click="emit('update:showShutdown', false)"
  >
    确定要关闭服务器吗？服务器将完全停止运行，您需要手动重新启动。
  </n-modal>
  <n-modal
    :show="showUpdateConfirm"
    preset="dialog"
    title="立即更新"
    type="warning"
    positive-text="开始更新"
    negative-text="取消"
    @update:show="emit('update:showUpdateConfirm', $event)"
    @positive-click="emit('applyUpdate')"
    @negative-click="emit('update:showUpdateConfirm', false)"
  >
    {{ updateConfirmText }}
  </n-modal>
  <n-modal
    :show="showRollbackConfirm"
    preset="dialog"
    title="回滚程序版本"
    type="warning"
    positive-text="开始回滚"
    negative-text="取消"
    @update:show="emit('update:showRollbackConfirm', $event)"
    @positive-click="emit('rollbackUpdate')"
    @negative-click="emit('update:showRollbackConfirm', false)"
  >
    {{ rollbackConfirmText }}
  </n-modal>
</template>

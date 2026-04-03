<script setup lang="ts">
import {
  NButton,
  NColorPicker,
  NDivider,
  NDrawer,
  NDrawerContent,
  NFormItem,
  NRadioButton,
  NRadioGroup,
  NSlider,
  NSpace,
} from 'naive-ui'

import { useUiStore, type ColorMode } from '@/stores/ui'

defineProps<{
  show: boolean
}>()

const emit = defineEmits<{
  (e: 'update:show', value: boolean): void
}>()

const ui = useUiStore()
const modeOptions: Array<{ label: string; value: ColorMode }> = [
  { label: '亮色', value: 'light' },
  { label: '暗色', value: 'dark' },
  { label: '跟随系统', value: 'auto' },
]

function close() {
  emit('update:show', false)
}
</script>

<template>
  <n-drawer :show="show" placement="right" :width="360" @update:show="emit('update:show', $event)">
    <n-drawer-content title="主题设置" :closable="true" @close="close">
      <n-space vertical :size="16">
        <n-form-item label="模式">
          <n-radio-group v-model:value="ui.mode">
            <n-radio-button
              v-for="opt in modeOptions"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </n-radio-group>
        </n-form-item>

        <n-divider />

        <n-form-item label="主色">
          <n-color-picker v-model:value="ui.primaryColor" :show-alpha="false" />
        </n-form-item>

        <n-form-item label="圆角">
          <n-slider v-model:value="ui.radius" :min="4" :max="18" :step="1" />
        </n-form-item>

        <n-form-item label="玻璃强度">
          <n-slider v-model:value="ui.glassBlur" :min="0" :max="40" :step="1" />
        </n-form-item>

        <n-divider />

        <n-space justify="end">
          <n-button @click="close">关闭</n-button>
        </n-space>
      </n-space>
    </n-drawer-content>
  </n-drawer>
</template>

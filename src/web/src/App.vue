<template>
  <el-config-provider :locale="locale">
    <div class="app-shell">
      <header class="topbar">
        <div class="brand">
          <span class="brand-mark" aria-hidden="true">CM</span>
          <span class="brand-name">Chromium Manager</span>
        </div>
        <div class="topbar-actions">
          <el-tooltip :content="themeLabel" placement="bottom">
            <button type="button" class="cm-icon-btn" :aria-label="themeLabel" @click="cycleTheme">
              <el-icon>
                <Monitor v-if="themeMode === 'auto'" />
                <Sunny v-else-if="themeMode === 'light'" />
                <Moon v-else />
              </el-icon>
            </button>
          </el-tooltip>
        </div>
      </header>
      <div class="app-body">
        <Left />
        <Right />
      </div>
    </div>
    <MapPicker />
  </el-config-provider>
</template>

<script setup>
import { computed, provide, reactive, ref } from 'vue'
import { ElConfigProvider } from 'element-plus'
import { Monitor, Sunny, Moon } from '@element-plus/icons-vue'
import zhCn from 'element-plus/es/locale/lang/zh-cn'

import Left from './components/left/LeftIndex.vue'
import Right from './components/right/RightIndex.vue'
import MapPicker from './components/MapPicker.vue'
import { themeMode, cycleTheme } from './utils/theme'

const locale = zhCn

const themeLabels = { auto: '主题：跟随系统', light: '主题：浅色', dark: '主题：深色' }
const themeLabel = computed(() => themeLabels[themeMode.value])

let activeGroupId = ref(null)
const updateActiveGroupId = (value) => {
  activeGroupId.value = value
}
provide('activeGroupId', activeGroupId)
provide('updateActiveGroupId', updateActiveGroupId)

let device = reactive({
  group: []
})
const updateDeviceGroup = (value) => {
  device.group = value
}
provide('device', device)
provide('updateDeviceGroup', updateDeviceGroup)

const getGroupName = (value) => {
  for (const item of device.group) {
    if (item._id === value) {
      return item.name
    }
  }
}
provide('getGroupName', getGroupName)
</script>

<style lang="scss" scoped>
.app-shell {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  overflow: hidden;
}

.topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex: 0 0 auto;
  height: $topbar-h;
  padding: 0 14px 0 16px;
  border-bottom: $border;
  background-color: $surface;
}

.brand {
  display: flex;
  align-items: center;
  gap: 10px;
}

.brand-mark {
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  border-radius: $radius-sm;
  color: #fff;
  background-color: $accent;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.3px;
}

.brand-name {
  color: $text;
  font-size: 14px;
  font-weight: 600;
}

.topbar-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.app-body {
  display: flex;
  flex: 1 1 auto;
  min-height: 0;
}
</style>

<style>
#app {
  width: 100%;
  height: 100%;
  overflow: hidden;
}
</style>

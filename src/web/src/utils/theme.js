import { ref, watch } from 'vue'

const STORAGE_KEY = 'cm-theme'
const media = window.matchMedia('(prefers-color-scheme: dark)')

// 'auto' 跟随系统，'light' / 'dark' 为手动固定
export const themeMode = ref(localStorage.getItem(STORAGE_KEY) || 'auto')

// 当前实际生效的是否为深色，供组件（如 leaflet 地图滤镜）判断
export const isDark = ref(false)

const apply = () => {
  const dark = themeMode.value === 'auto' ? media.matches : themeMode.value === 'dark'
  isDark.value = dark
  document.documentElement.classList.toggle('dark', dark)
}

watch(themeMode, (mode) => {
  if (mode === 'auto') {
    localStorage.removeItem(STORAGE_KEY)
  } else {
    localStorage.setItem(STORAGE_KEY, mode)
  }
  apply()
})

media.addEventListener('change', () => {
  if (themeMode.value === 'auto') apply()
})

apply()

// 三态循环：auto → light → dark → auto
export const cycleTheme = () => {
  const order = ['auto', 'light', 'dark']
  themeMode.value = order[(order.indexOf(themeMode.value) + 1) % order.length]
}

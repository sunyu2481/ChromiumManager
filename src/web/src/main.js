import { createApp } from 'vue'
import App from './App.vue'
import 'element-plus/dist/index.css'
// 深色变量必须在自定义样式之前引入，才能被 style.scss 里的令牌覆盖
import 'element-plus/theme-chalk/dark/css-vars.css'
import './assets/css/style.scss'
import './utils/theme'

const app = createApp(App)

app.mount('#app')

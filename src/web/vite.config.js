import { resolve } from 'path'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import eslint from 'vite-plugin-eslint2'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'

// https://vitejs.dev/config/
export default defineConfig({
  base: './',
  plugins: [
    vue(),
    eslint({
      cache: false,
      fix: false
    }),
    Components({
      resolvers: [ElementPlusResolver()]
    })
  ],
  resolve: {
    alias: {
      '@': resolve('./src')
    }
  },
  css: {
    preprocessorOptions: {
      scss: {
        additionalData: `@use "${resolve('./src/assets/css/_variables.scss').replace(/\\/g, '/')}" as *;`
      }
    }
  },
  // 开发模式下把 API 请求透传给 Go 服务，避免 CORS 拦截
  server: {
    proxy: {
      '/get_': 'http://127.0.0.1:10101',
      '/add_': 'http://127.0.0.1:10101',
      '/update_': 'http://127.0.0.1:10101',
      '/delete_': 'http://127.0.0.1:10101',
      '/launch_': 'http://127.0.0.1:10101',
      '/stop_': 'http://127.0.0.1:10101',
      '/show_': 'http://127.0.0.1:10101',
      '/export_': 'http://127.0.0.1:10101',
      '/import_': 'http://127.0.0.1:10101',
      '/events': { target: 'http://127.0.0.1:10101', ws: true }
    }
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          vue: ['vue'],
          'element-plus': ['element-plus', '@element-plus/icons-vue']
        }
      }
    },
    chunkSizeWarningLimit: 500
  }
})

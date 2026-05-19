import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@wailsjs': resolve(__dirname, 'wailsjs')
    }
  },
  define: {
    __BUILD_TIME__: JSON.stringify(new Date().toLocaleString('zh-CN'))
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})

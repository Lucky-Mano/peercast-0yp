import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  base: '/yp/',
  build: {
    outDir: '../internal/httpd/dist/web',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/yp/api': { target: 'https://localhost', secure: false },
      '/yp/index.txt': { target: 'https://localhost', secure: false },
    },
  },
})

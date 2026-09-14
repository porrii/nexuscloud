import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    // El backend Go embebe este directorio con go:embed y lo sirve cuando
    // web.enabled=true (ver internal/webui).
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    // En desarrollo, Vite sirve la SPA en su propio puerto pero reenvía
    // las llamadas a la API al backend Go real, así el navegador ve todo
    // como same-origin (cookies de sesión incluidas) igual que en
    // producción, donde ambos los sirve el mismo binario.
    proxy: {
      '/api': 'http://localhost:8080',
      '/health': 'http://localhost:8080',
      '/ready': 'http://localhost:8080',
    },
  },
})

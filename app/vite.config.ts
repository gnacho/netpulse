import path from "path"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"
import { VitePWA } from "vite-plugin-pwa"

// https://vite.dev/config/
export default defineConfig({
  // Servido desde el backend Hono en la raíz del dominio → rutas absolutas.
  // (Con './' las rutas anidadas tipo /routers/:id rompen los assets al recargar.)
  base: '/',
  plugins: [
    react(),
    VitePWA({
      registerType: 'autoUpdate',
      includeAssets: ['logo.svg', 'apple-touch-icon.png', 'icon-192.png', 'icon-512.png'],
      manifest: {
        name: 'NetPulse',
        short_name: 'NetPulse',
        description: 'Panel de control de tu red doméstica',
        lang: 'es',
        display: 'standalone',
        orientation: 'any',
        theme_color: '#070B12',
        background_color: '#070B12',
        categories: ['utilities'],
        icons: [
          { src: 'icon-192.png', sizes: '192x192', type: 'image/png', purpose: 'maskable any' },
          { src: 'icon-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable any' },
        ],
      },
      workbox: {
        // App-shell precache; los datos son mock locales (siempre "frescos")
        globPatterns: ['**/*.{js,css,html,svg,png,woff2}'],
        navigateFallback: 'index.html',
      },
    }),
  ],
  server: {
    // El backend Hono usa el puerto 3000 (contrato API); Vite dev va en 5173
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:3000',
        changeOrigin: true,
      },
    },
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
});

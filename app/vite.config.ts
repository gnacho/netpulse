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
      // injectManifest: SW propio (src/sw.ts) con handlers Web Push;
      // generateSW no permite handlers custom (SPEC-PUSH §2).
      strategies: 'injectManifest',
      srcDir: 'src',
      filename: 'sw.ts',
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
      injectManifest: {
        // App-shell precache (mismo glob que con generateSW: 16 entradas);
        // el fallback de navegación vive ahora en src/sw.ts (NavigationRoute)
        globPatterns: ['**/*.{js,css,html,svg,png,woff2}'],
      },
      // SW también en dev: permite probar el alta push en local (localhost
      // es contexto seguro). La ruta de navegación offline se desactiva en
      // dev dentro de src/sw.ts para no congelar el index.html.
      devOptions: {
        enabled: true,
        type: 'module',
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

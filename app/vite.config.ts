import path from "path"
import react from "@vitejs/plugin-react"
import { defineConfig, type Plugin } from "vite"
import { VitePWA } from "vite-plugin-pwa"

// Tracker GoatCounter SOLO en el build de la demo pública (issue #240):
//   VITE_GC_COUNT=https://stats.netpulse.cloudless.club npm run build
// Los builds normales NO lo llevan: una instalación self-hosted nunca debe
// llamar a casa. Los hits se registran con prefijo /demo en el mismo site
// que la landing ("/" = landing, "/demo/..." = demo).
const gcCount = process.env.VITE_GC_COUNT?.replace(/\/$/, "")

function goatcounterPlugin(): Plugin {
  return {
    name: "netpulse-goatcounter",
    transformIndexHtml(html) {
      if (!gcCount) return html
      const snippet =
        `    <script>window.goatcounter={path:function(p){return '/demo'+p}}</script>\n` +
        `    <script async data-goatcounter="${gcCount}/count" src="${gcCount}/count.js"></script>\n  </head>`
      return html.replace("</head>", snippet)
    },
  }
}

// https://vite.dev/config/
export default defineConfig({
  // Servido desde el backend Hono en la raíz del dominio → rutas absolutas.
  // (Con './' las rutas anidadas tipo /routers/:id rompen los assets al recargar.)
  base: '/',
  plugins: [
    react(),
    goatcounterPlugin(),
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

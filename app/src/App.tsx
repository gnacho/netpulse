import { Route, Routes } from 'react-router'
import { Navigate } from 'react-router'
import { AuthGate } from '@/components/AuthGate'
import Layout from '@/components/Layout'
import { DataProvider } from '@/data/DataProvider'
import Home from '@/pages/Home'
import Login from '@/pages/Login'
import Routers from '@/pages/Routers'
import RouterDetail from '@/pages/RouterDetail'
import Devices from '@/pages/Devices'
import Topology from '@/pages/Topology'
import Alerts from '@/pages/Alerts'
import Roaming from '@/pages/Roaming'
import Reports from '@/pages/Reports'
import Orchestration from '@/pages/Orchestration'
import Settings from '@/pages/Settings'
import Placeholder from '@/pages/Placeholder'

/**
 * Jerarquía (webapp-stack §Patrón frontend):
 *   ThemeProvider (inline en main.tsx) > AuthGate > DataProvider > Layout > Routes
 * La ruta `/login` queda FUERA del Layout y del gate.
 */
export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route
        element={
          <AuthGate>
            <DataProvider>
              <Layout />
            </DataProvider>
          </AuthGate>
        }
      >
        <Route index element={<Home />} />
        <Route path="routers" element={<Routers />} />
        <Route path="routers/:id" element={<RouterDetail />} />
        {/* La vista de agentes vive ahora como sección de /routers (#284):
         * el route legacy redirige para no romper enlaces antiguos. */}
        <Route path="agents" element={<Navigate to="/routers" replace />} />
        <Route path="devices" element={<Devices />} />
        <Route path="topology" element={<Topology />} />
        <Route path="alerts" element={<Alerts />} />
        <Route path="roaming" element={<Roaming />} />
        <Route path="reports" element={<Reports />} />
        <Route path="orchestration" element={<Orchestration />} />
        <Route path="settings" element={<Settings />} />
        <Route path="*" element={<Placeholder title="Página no encontrada" />} />
      </Route>
    </Routes>
  )
}

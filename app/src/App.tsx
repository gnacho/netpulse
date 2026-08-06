import { Route, Routes } from 'react-router'
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
        <Route path="devices" element={<Devices />} />
        <Route path="topology" element={<Topology />} />
        <Route path="alerts" element={<Alerts />} />
        <Route path="settings" element={<Settings />} />
        <Route path="*" element={<Placeholder title="Página no encontrada" />} />
      </Route>
    </Routes>
  )
}

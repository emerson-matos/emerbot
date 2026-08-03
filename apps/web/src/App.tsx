import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import AppLayout from './components/layout/AppLayout';
import Transactions from './pages/Transactions';
import NovaTransacao from './pages/NovaTransacao';
import EditarTransacao from './pages/EditarTransacao';
import Goals from './pages/Goals';
import Adquirentes from './pages/Adquirentes';
import Analysis from './pages/Analysis';
import Notificacoes from './pages/Notificacoes';
import Settings from './pages/Settings';
import { AuthLayout } from './components/layout/AuthLayout';
import { AuthProvider, ProtectedRoute } from '@/lib/auth'

export default function App() {
  return (
    <AuthProvider>
      <BrowserRouter>
        <Routes>

          <Route
            element={
              <AuthLayout />
            }
          >
            <Route path="/login" element={<Login />} />
          </Route>

          <Route element={<ProtectedRoute />}>
            <Route element={<AppLayout />}>
              <Route index element={<Dashboard />} />
              <Route path="adquirentes" element={<Adquirentes />} />
              <Route path="ajustes" element={<Settings />} />
              <Route path="analise" element={<Analysis />} />
              <Route path="metas" element={<Goals />} />
              <Route path="notificacoes" element={<Notificacoes />} />
              <Route path="nova-transacao" element={<NovaTransacao />} />
              <Route path="transacoes" element={<Transactions />} />
              <Route path="transacoes/:date/:id/editar" element={<EditarTransacao />} />
            </Route>
          </Route >

          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes >
      </BrowserRouter >
    </AuthProvider >
  )
}

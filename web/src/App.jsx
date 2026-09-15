import { BrowserRouter, Routes, Route, Navigate, Outlet, useLocation } from 'react-router-dom'
import { UserProvider, useUser, Layout } from './components/Layout'
import { ErrorBoundary } from './components/ErrorBoundary'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Nodes from './pages/Nodes'
import Servers from './pages/Servers'
import Users from './pages/Users'
import Packages from './pages/Packages'
import Forwards from './pages/Forwards'
import Traffic from './pages/Traffic'
import Settings from './pages/Settings'
import My from './pages/My'
import MyNodes from './pages/MyNodes'
import MyForwards from './pages/MyForwards'
import { BrandMark } from './components/ui'

function Loading() {
  return (
    <div className="min-h-dvh grid place-items-center">
      <div>
        <BrandMark size={36} className="mb-3" />
        <div className="text-[15px] font-semibold">liking</div>
        <div className="text-[12px] text-ink-mut mt-1">加载中…</div>
      </div>
    </div>
  )
}

function AuthLayout() {
  const { user } = useUser()
  const loc = useLocation()
  if (user === undefined) return <Loading />
  if (user === null) return <Navigate to="/login" replace />
  return (
    <Layout>
      <ErrorBoundary key={loc.pathname}>
        <Outlet />
      </ErrorBoundary>
    </Layout>
  )
}

function RequireAdmin() {
  const { user } = useUser()
  if (user?.role !== 'admin') return <Navigate to="/my" replace />
  return <Outlet />
}

function MySettings() {
  const { user } = useUser()
  if (user?.role === 'admin') return <Navigate to="/settings?tab=account" replace />
  return <Settings accountOnly />
}

export default function App() {
  return (
    <BrowserRouter useTransitions={false}>
      <UserProvider>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route element={<AuthLayout />}>
            <Route path="/my" element={<My />} />
            <Route path="/my/settings" element={<MySettings />} />
            <Route path="/my/password" element={<MySettings />} />
            <Route element={<RequireAdmin />}>
              <Route path="/" element={<Dashboard />} />
              <Route path="/nodes" element={<Nodes />} />
              <Route path="/servers" element={<Servers />} />
              <Route path="/inbounds" element={<Navigate to="/nodes" replace />} />
              <Route path="/users" element={<Users />} />
              <Route path="/packages" element={<Packages />} />
              <Route path="/forwards" element={<Forwards />} />
              <Route path="/traffic" element={<Traffic />} />
              <Route path="/certs" element={<Navigate to="/settings?tab=certs" replace />} />
              <Route path="/settings" element={<Settings />} />
              <Route path="/password" element={<Navigate to="/settings?tab=account" replace />} />
              <Route path="/my/nodes" element={<MyNodes />} />
              <Route path="/my/forwards" element={<MyForwards />} />
            </Route>
          </Route>
          <Route path="*" element={
            <div className="min-h-dvh grid place-items-center">
              <div className="text-center">
                <div className="text-[12px] text-ink-mut">页面不存在</div>
                <div className="text-[28px] font-semibold mt-1">404</div>
                <a href="/" className="linkish mt-3 inline-block">返回</a>
              </div>
            </div>
          } />
        </Routes>
      </UserProvider>
    </BrowserRouter>
  )
}

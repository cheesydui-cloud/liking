import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { UserProvider, useUser, Layout } from './components/Layout'
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

function AdminRoute({ children }) {
  const { user } = useUser()
  if (user === undefined) return <Loading />
  if (user === null) return <Navigate to="/login" replace />
  if (user.role !== 'admin') return <Navigate to="/my" replace />
  return <Layout>{children}</Layout>
}

function UserRoute({ children }) {
  const { user } = useUser()
  if (user === undefined) return <Loading />
  if (user === null) return <Navigate to="/login" replace />
  return <Layout>{children}</Layout>
}

function Root() {
  const { user } = useUser()
  if (user === undefined) return <Loading />
  if (user === null) return <Navigate to="/login" replace />
  if (user.role !== 'admin') return <Navigate to="/my" replace />
  return <Layout><Dashboard /></Layout>
}

export default function App() {
  return (
    <BrowserRouter>
      <UserProvider>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/" element={<Root />} />
          <Route path="/nodes" element={<AdminRoute><Nodes /></AdminRoute>} />
          <Route path="/servers" element={<AdminRoute><Servers /></AdminRoute>} />
          <Route path="/inbounds" element={<Navigate to="/nodes" replace />} />
          <Route path="/users" element={<AdminRoute><Users /></AdminRoute>} />
          <Route path="/packages" element={<AdminRoute><Packages /></AdminRoute>} />
          <Route path="/forwards" element={<AdminRoute><Forwards /></AdminRoute>} />
          <Route path="/traffic" element={<AdminRoute><Traffic /></AdminRoute>} />
          <Route path="/certs" element={<Navigate to="/settings?tab=certs" replace />} />
          <Route path="/settings" element={<AdminRoute><Settings /></AdminRoute>} />
          <Route path="/password" element={<Navigate to="/settings?tab=account" replace />} />
          <Route path="/my" element={<UserRoute><My /></UserRoute>} />
          <Route path="/my/settings" element={<UserRoute><Settings accountOnly /></UserRoute>} />
          <Route path="/my/password" element={<Navigate to="/my/settings" replace />} />
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

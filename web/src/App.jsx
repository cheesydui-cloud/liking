import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { UserProvider, useUser, Layout } from './components/Layout'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Servers from './pages/Servers'
import Inbounds from './pages/Inbounds'
import Users from './pages/Users'
import Packages from './pages/Packages'
import Certs from './pages/Certs'
import Settings from './pages/Settings'
import My from './pages/My'
import Password from './pages/Password'

function Loading() {
  return (
    <div className="min-h-dvh grid place-items-center">
      <div className="text-center">
        <div className="mx-auto w-11 h-11 rounded-xl grid place-items-center font-display text-[20px] mb-4"
          style={{ color: 'var(--color-gold)', border: '1px solid color-mix(in srgb, var(--color-gold) 45%, transparent)' }}>L</div>
        <div className="font-display text-[32px] leading-none">liking</div>
        <div className="kicker mt-3">Loading</div>
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
  if (user.role === 'admin') return <Navigate to="/" replace />
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
          <Route path="/servers" element={<AdminRoute><Servers /></AdminRoute>} />
          <Route path="/inbounds" element={<AdminRoute><Inbounds /></AdminRoute>} />
          <Route path="/users" element={<AdminRoute><Users /></AdminRoute>} />
          <Route path="/packages" element={<AdminRoute><Packages /></AdminRoute>} />
          <Route path="/certs" element={<AdminRoute><Certs /></AdminRoute>} />
          <Route path="/settings" element={<AdminRoute><Settings /></AdminRoute>} />
          <Route path="/password" element={<AdminRoute><Password /></AdminRoute>} />
          <Route path="/my" element={<UserRoute><My /></UserRoute>} />
          <Route path="/my/password" element={<UserRoute><Password /></UserRoute>} />
          <Route path="*" element={
            <div className="min-h-dvh grid place-items-center">
              <div className="text-center">
                <div className="kicker">Lost</div>
                <div className="font-display text-[48px]">404</div>
                <a href="/" className="linkish mt-3 inline-block">返回</a>
              </div>
            </div>
          } />
        </Routes>
      </UserProvider>
    </BrowserRouter>
  )
}

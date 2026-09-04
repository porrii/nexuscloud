import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { AuthProvider } from './auth/AuthContext'
import RequireAuth from './auth/RequireAuth'
import AppShell from './components/AppShell'
import AccountPage from './pages/AccountPage'
import FilesPage from './pages/FilesPage'
import LoginPage from './pages/LoginPage'
import TrashPage from './pages/TrashPage'

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route
            element={
              <RequireAuth>
                <AppShell />
              </RequireAuth>
            }
          >
            <Route path="/" element={<FilesPage />} />
            <Route path="/trash" element={<TrashPage />} />
            <Route path="/account" element={<AccountPage />} />
          </Route>
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  )
}

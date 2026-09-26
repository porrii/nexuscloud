import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { AuthProvider } from './auth/AuthContext'
import RequireAuth from './auth/RequireAuth'
import AppShell from './components/AppShell'
import AccountPage from './pages/AccountPage'
import AnonymousUploadPage from './pages/AnonymousUploadPage'
import FavoritesPage from './pages/FavoritesPage'
import FilesPage from './pages/FilesPage'
import LoginPage from './pages/LoginPage'
import PublicSharePage from './pages/PublicSharePage'
import SharedPage from './pages/SharedPage'
import TrashPage from './pages/TrashPage'

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          {/* Enlaces públicos (§37): sin sesión, fuera del shell autenticado -- ver docs/storage.md#compartición-37. */}
          <Route path="/s/:token" element={<PublicSharePage />} />
          {/* Subida anónima (§38, ADR-039): sin sesión, modelo separado de /s/:token. */}
          <Route path="/u/:token" element={<AnonymousUploadPage />} />
          <Route
            element={
              <RequireAuth>
                <AppShell />
              </RequireAuth>
            }
          >
            <Route path="/" element={<FilesPage />} />
            <Route path="/shared" element={<SharedPage />} />
            <Route path="/favorites" element={<FavoritesPage />} />
            <Route path="/trash" element={<TrashPage />} />
            <Route path="/account" element={<AccountPage />} />
          </Route>
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  )
}

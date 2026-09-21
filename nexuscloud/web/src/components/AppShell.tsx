import { NavLink, Outlet } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import UsageBar from './UsageBar'

const navLinkClasses = ({ isActive }: { isActive: boolean }) =>
  `flex items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition ${
    isActive
      ? 'bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300'
      : 'text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800'
  }`

export default function AppShell() {
  const { user, logout } = useAuth()

  return (
    <div className="flex h-screen bg-slate-50 dark:bg-slate-950">
      <aside className="flex w-60 shrink-0 flex-col border-r border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
        <div className="px-4 py-5">
          <h1 className="text-lg font-semibold text-slate-900 dark:text-slate-50">NexusCloud</h1>
        </div>

        <nav className="flex flex-col gap-1 px-3">
          <NavLink to="/" end className={navLinkClasses}>
            Mis archivos
          </NavLink>
          <NavLink to="/shared" className={navLinkClasses}>
            Compartido
          </NavLink>
          <NavLink to="/trash" className={navLinkClasses}>
            Papelera
          </NavLink>
          <NavLink to="/account" className={navLinkClasses}>
            Cuenta
          </NavLink>
        </nav>

        <div className="mt-auto">
          <UsageBar />
          <div className="border-t border-slate-200 p-3 dark:border-slate-800">
            <p className="truncate px-1 text-sm font-medium text-slate-800 dark:text-slate-200">{user?.display_name}</p>
            <p className="truncate px-1 text-xs text-slate-500 dark:text-slate-400">@{user?.username}</p>
            <button
              onClick={() => void logout()}
              className="mt-2 w-full rounded-md px-3 py-1.5 text-left text-sm text-slate-600 transition hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
            >
              Cerrar sesión
            </button>
          </div>
        </div>
      </aside>

      <main className="flex-1 overflow-auto">
        <Outlet />
      </main>
    </div>
  )
}

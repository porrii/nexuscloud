import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { ApiClientError } from '../api/client'
import { useAuth } from '../auth/AuthContext'

export default function LoginPage() {
  const { login } = useAuth()
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [totpCode, setTotpCode] = useState('')
  const [needsTotp, setNeedsTotp] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await login(username, password, totpCode || undefined)
      navigate('/', { replace: true })
    } catch (err) {
      if (err instanceof ApiClientError && err.code === 'totp_required') {
        setNeedsTotp(true)
        setError('Introduce tu código de verificación en dos pasos.')
      } else if (err instanceof ApiClientError) {
        setError(err.message)
      } else {
        setError('No se pudo conectar con el servidor.')
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-100 dark:bg-slate-950">
      <form
        onSubmit={handleSubmit}
        className="w-full max-w-sm rounded-xl border border-slate-200 bg-white p-8 shadow-sm dark:border-slate-800 dark:bg-slate-900"
      >
        <h1 className="mb-1 text-xl font-semibold text-slate-900 dark:text-slate-50">NexusCloud</h1>
        <p className="mb-6 text-sm text-slate-500 dark:text-slate-400">Inicia sesión para continuar</p>

        <label className="mb-1 block text-sm font-medium text-slate-700 dark:text-slate-300" htmlFor="username">
          Usuario
        </label>
        <input
          id="username"
          className="mb-4 w-full rounded-md border border-slate-300 px-3 py-2 text-sm outline-none focus:border-blue-500 focus:ring-1 focus:ring-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          autoComplete="username"
          autoFocus
          required
        />

        <label className="mb-1 block text-sm font-medium text-slate-700 dark:text-slate-300" htmlFor="password">
          Contraseña
        </label>
        <input
          id="password"
          type="password"
          className="mb-4 w-full rounded-md border border-slate-300 px-3 py-2 text-sm outline-none focus:border-blue-500 focus:ring-1 focus:ring-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
          required
        />

        {needsTotp && (
          <>
            <label className="mb-1 block text-sm font-medium text-slate-700 dark:text-slate-300" htmlFor="totp">
              Código de verificación
            </label>
            <input
              id="totp"
              className="mb-4 w-full rounded-md border border-slate-300 px-3 py-2 text-sm tracking-widest outline-none focus:border-blue-500 focus:ring-1 focus:ring-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
              value={totpCode}
              onChange={(e) => setTotpCode(e.target.value)}
              inputMode="numeric"
              maxLength={6}
              autoFocus
            />
          </>
        )}

        {error && <p className="mb-4 text-sm text-red-600 dark:text-red-400">{error}</p>}

        <button
          type="submit"
          disabled={submitting}
          className="w-full rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-blue-700 disabled:opacity-60"
        >
          {submitting ? 'Entrando…' : 'Entrar'}
        </button>
      </form>
    </div>
  )
}

import { useCallback, useEffect, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { api, type Quota } from '../api/client'
import { formatBytes } from '../format'
import { USAGE_CHANGED_EVENT, usageLevel, usageRatio } from '../quota'

const BAR_COLOR = { none: 'bg-blue-500', ok: 'bg-blue-500', warn: 'bg-amber-500', full: 'bg-red-500' } as const

function sourceLabel(q: Quota): string {
  switch (q.source) {
    case 'user':
      return q.limit_bytes === undefined ? 'Sin límite (cuenta sin cuota)' : 'Cuota de tu cuenta'
    case 'group':
      return q.limit_bytes === undefined
        ? `Sin límite (grupo ${q.group_name ?? ''})`
        : `Cuota del grupo ${q.group_name ?? ''}`
    case 'global':
      return 'Cuota general del servidor'
    default:
      return 'Sin límite'
  }
}

/**
 * Uso de almacenamiento de quien está conectado (§24), en la barra lateral: lo
 * que ocupa —archivos, papelera y versiones anteriores— frente a su cuota. Se
 * actualiza al montar, al cambiar de página y cuando alguna página avisa de
 * que cambió lo que ocupa (USAGE_CHANGED_EVENT). Si no se puede consultar no
 * muestra nada: es informativa y nunca debe estorbar.
 */
export default function UsageBar() {
  const [quota, setQuota] = useState<Quota | null>(null)
  const { pathname } = useLocation()
  const timer = useRef<number | undefined>(undefined)

  const refresh = useCallback(() => {
    api
      .quota()
      .then(setQuota)
      .catch(() => setQuota(null))
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh, pathname])

  useEffect(() => {
    // Varias subidas seguidas lanzan varios avisos: se agrupan en una consulta.
    const onChange = () => {
      window.clearTimeout(timer.current)
      timer.current = window.setTimeout(refresh, 250)
    }
    window.addEventListener(USAGE_CHANGED_EVENT, onChange)
    return () => {
      window.removeEventListener(USAGE_CHANGED_EVENT, onChange)
      window.clearTimeout(timer.current)
    }
  }, [refresh])

  if (!quota) return null

  const level = usageLevel(quota)
  const ratio = usageRatio(quota)
  const percent = ratio === null ? 0 : Math.min(100, Math.round(ratio * 100))
  const breakdown = `Archivos ${formatBytes(quota.files_bytes)} · Papelera ${formatBytes(quota.trash_bytes)} · Versiones ${formatBytes(quota.versions_bytes)}`

  return (
    <div className="px-4 pb-3" title={`${breakdown}\n${sourceLabel(quota)}`} data-testid="usage-bar" data-level={level}>
      <p className="text-xs font-medium text-slate-600 dark:text-slate-300">Almacenamiento</p>
      {quota.limit_bytes !== undefined && (
        <div
          role="progressbar"
          aria-label="Espacio usado"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={percent}
          className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-slate-200 dark:bg-slate-700"
        >
          <div className={`h-full rounded-full transition-all ${BAR_COLOR[level]}`} style={{ width: `${percent}%` }} />
        </div>
      )}
      <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">
        {quota.limit_bytes === undefined
          ? `${formatBytes(quota.used_bytes)} usados`
          : `${formatBytes(quota.used_bytes)} de ${formatBytes(quota.limit_bytes)}`}
      </p>
      {level === 'full' && (
        <p className="mt-0.5 text-xs font-medium text-red-600 dark:text-red-400">
          {(ratio ?? 0) >= 1 ? 'Almacenamiento lleno' : 'Casi sin espacio'}
        </p>
      )}
    </div>
  )
}

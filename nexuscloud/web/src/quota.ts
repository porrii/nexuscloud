import type { Quota } from './api/client'

/**
 * Evento que las páginas lanzan cuando cambia lo que ocupa el usuario (subir,
 * borrar del todo...): la barra de uso de la barra lateral lo escucha y vuelve
 * a pedir la cuota, sin que las páginas tengan que conocerla.
 */
export const USAGE_CHANGED_EVENT = 'nexuscloud:usage-changed'

export function notifyUsageChanged(): void {
  window.dispatchEvent(new Event(USAGE_CHANGED_EVENT))
}

/** none = sin límite; ok < 80 %; warn de 80 % a 95 %; full desde 95 %. */
export type UsageLevel = 'none' | 'ok' | 'warn' | 'full'

/** Fracción usada (0..1 o más si está por encima); null si no hay límite. */
export function usageRatio(q: Quota): number | null {
  if (q.limit_bytes === undefined || q.limit_bytes <= 0) return null
  return q.used_bytes / q.limit_bytes
}

export function usageLevel(q: Quota): UsageLevel {
  const ratio = usageRatio(q)
  if (ratio === null) return 'none'
  if (ratio >= 0.95) return 'full'
  if (ratio >= 0.8) return 'warn'
  return 'ok'
}

const UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']

/** Tamaño legible en unidades binarias (1 KB = 1024 B), igual que el resto de la interfaz. */
export function formatBytes(bytes: number): string {
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < UNITS.length - 1) {
    value /= 1024
    unit++
  }
  return `${value.toFixed(unit > 0 && value < 10 ? 1 : 0)} ${UNITS[unit]}`
}

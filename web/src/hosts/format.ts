export function formatBytes(value: number | null): string {
  if (value === null) return 'Sin dato'
  if (value === 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  return `${new Intl.NumberFormat('es-CL', { maximumFractionDigits: 1 }).format(value / 1024 ** index)} ${units[index]}`
}

export function formatPercent(value: number | null): string {
  return value === null
    ? 'Sin dato'
    : `${new Intl.NumberFormat('es-CL', { maximumFractionDigits: 1 }).format(value)} %`
}

export function formatDate(value: string | null): string {
  if (value === null) return 'Sin señal todavía'
  return new Intl.DateTimeFormat('es-CL', { dateStyle: 'medium', timeStyle: 'medium' }).format(
    new Date(value),
  )
}

export function formatDuration(value: number | null): string {
  if (value === null) return 'Sin dato'
  if (value < 60) return `${value} s`
  if (value < 3600) return `${Math.floor(value / 60)} min`
  if (value < 86400) return `${Math.floor(value / 3600)} h`
  return `${Math.floor(value / 86400)} d ${Math.floor((value % 86400) / 3600)} h`
}

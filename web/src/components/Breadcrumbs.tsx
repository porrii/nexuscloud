interface BreadcrumbsProps {
  path: string
  onNavigate: (path: string) => void
}

/** Descompone "/Documentos/Proyectos" en segmentos navegables. */
export default function Breadcrumbs({ path, onNavigate }: BreadcrumbsProps) {
  const segments = path.split('/').filter(Boolean)

  return (
    <nav className="flex flex-wrap items-center gap-1 text-sm text-slate-600 dark:text-slate-300">
      <button onClick={() => onNavigate('/')} className="rounded px-1.5 py-0.5 font-medium hover:bg-slate-100 dark:hover:bg-slate-800">
        Mis archivos
      </button>
      {segments.map((segment, i) => {
        const segmentPath = '/' + segments.slice(0, i + 1).join('/')
        return (
          <span key={segmentPath} className="flex items-center gap-1">
            <span className="text-slate-400 dark:text-slate-600">/</span>
            <button onClick={() => onNavigate(segmentPath)} className="rounded px-1.5 py-0.5 hover:bg-slate-100 dark:hover:bg-slate-800">
              {segment}
            </button>
          </span>
        )
      })}
    </nav>
  )
}

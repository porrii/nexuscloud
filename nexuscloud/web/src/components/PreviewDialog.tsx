import { useEffect, useState } from 'react'
import DOMPurify from 'dompurify'
import hljs from 'highlight.js/lib/core'
import bash from 'highlight.js/lib/languages/bash'
import cpp from 'highlight.js/lib/languages/cpp'
import csharp from 'highlight.js/lib/languages/csharp'
import css from 'highlight.js/lib/languages/css'
import go from 'highlight.js/lib/languages/go'
import java from 'highlight.js/lib/languages/java'
import javascript from 'highlight.js/lib/languages/javascript'
import json from 'highlight.js/lib/languages/json'
import php from 'highlight.js/lib/languages/php'
import python from 'highlight.js/lib/languages/python'
import ruby from 'highlight.js/lib/languages/ruby'
import rust from 'highlight.js/lib/languages/rust'
import sql from 'highlight.js/lib/languages/sql'
import typescript from 'highlight.js/lib/languages/typescript'
import xml from 'highlight.js/lib/languages/xml'
import yaml from 'highlight.js/lib/languages/yaml'
import { marked } from 'marked'
import { api, ApiClientError, type FileEntry } from '../api/client'

hljs.registerLanguage('bash', bash)
hljs.registerLanguage('cpp', cpp)
hljs.registerLanguage('csharp', csharp)
hljs.registerLanguage('css', css)
hljs.registerLanguage('go', go)
hljs.registerLanguage('java', java)
hljs.registerLanguage('javascript', javascript)
hljs.registerLanguage('json', json)
hljs.registerLanguage('php', php)
hljs.registerLanguage('python', python)
hljs.registerLanguage('ruby', ruby)
hljs.registerLanguage('rust', rust)
hljs.registerLanguage('sql', sql)
hljs.registerLanguage('typescript', typescript)
hljs.registerLanguage('xml', xml)
hljs.registerLanguage('yaml', yaml)

// Extensión -> lenguaje de highlight.js registrado arriba (§35): un
// subconjunto curado, no las ~190 gramáticas de highlight.js completo --
// mismo criterio de dependencias ligeras que pidió el plan de esta fase.
const CODE_LANGUAGE_BY_EXT: Record<string, string> = {
  sh: 'bash',
  bash: 'bash',
  zsh: 'bash',
  c: 'cpp',
  h: 'cpp',
  cpp: 'cpp',
  cc: 'cpp',
  hpp: 'cpp',
  cs: 'csharp',
  css: 'css',
  scss: 'css',
  go: 'go',
  java: 'java',
  js: 'javascript',
  jsx: 'javascript',
  mjs: 'javascript',
  cjs: 'javascript',
  json: 'json',
  php: 'php',
  py: 'python',
  rb: 'ruby',
  rs: 'rust',
  sql: 'sql',
  ts: 'typescript',
  tsx: 'typescript',
  html: 'xml',
  htm: 'xml',
  xml: 'xml',
  svg: 'xml',
  yml: 'yaml',
  yaml: 'yaml',
}

// Por debajo de este tamaño, texto/Markdown/código se cargan enteros en
// memoria del navegador (§35) -- imagen/PDF/vídeo/audio no lo necesitan,
// el navegador ya los sirve en streaming igual que una descarga.
const MAX_PREVIEW_BYTES = 5 * 1024 * 1024

type PreviewKind = 'image' | 'pdf' | 'video' | 'audio' | 'markdown' | 'code' | 'text' | 'unsupported'

function extensionOf(name: string): string {
  const dot = name.lastIndexOf('.')
  return dot === -1 ? '' : name.slice(dot + 1).toLowerCase()
}

function kindOf(file: FileEntry): { kind: PreviewKind; language?: string } {
  const ext = extensionOf(file.name)
  if (file.mime_type.startsWith('image/')) return { kind: 'image' }
  if (file.mime_type === 'application/pdf') return { kind: 'pdf' }
  if (file.mime_type.startsWith('video/')) return { kind: 'video' }
  if (file.mime_type.startsWith('audio/')) return { kind: 'audio' }
  if (ext === 'md' || ext === 'markdown') return { kind: 'markdown' }
  const language = CODE_LANGUAGE_BY_EXT[ext]
  if (language) return { kind: 'code', language }
  if (file.mime_type.startsWith('text/')) return { kind: 'text' }
  return { kind: 'unsupported' }
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let value = bytes / 1024
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value.toFixed(value < 10 ? 1 : 0)} ${units[unit]}`
}

interface PreviewDialogProps {
  file: FileEntry
  onClose: () => void
}

/**
 * Previsualización de archivos (§35, §138, ADR-040 Fase 2). Sin endpoint
 * propio: reutiliza GET /files/{id} tal cual (streaming, Content-Type ya
 * correcto). Imagen/PDF/vídeo/audio los renderiza el propio navegador;
 * Markdown y código son la primera vez que NexusCloud interpreta contenido
 * de usuario en cualquier capa, así que ambos pasan por una vía que
 * produce HTML ya seguro antes de tocar el DOM (ver MarkdownPreview/
 * CodePreview más abajo) en vez de inyectar el archivo tal cual.
 */
export default function PreviewDialog({ file, onClose }: PreviewDialogProps) {
  const { kind, language } = kindOf(file)
  const needsText = kind === 'markdown' || kind === 'code' || kind === 'text'
  const tooLarge = needsText && file.size_bytes > MAX_PREVIEW_BYTES

  const [text, setText] = useState<string | null>(null)
  const [loading, setLoading] = useState(needsText && !tooLarge)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!needsText || tooLarge) return
    let cancelled = false
    setLoading(true)
    setError(null)
    setText(null)
    api
      .fetchPreviewText(file.id)
      .then((content) => {
        if (!cancelled) setText(content)
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof ApiClientError ? err.message : 'No se pudo cargar el contenido.')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
    // needsText/tooLarge/kind se derivan de file, así que solo hace falta
    // volver a cargar cuando cambia el propio archivo (mismo criterio que
    // SearchPage.tsx con searchParams).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [file.id])

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={onClose}>
      <div
        className="flex max-h-[85vh] w-full max-w-4xl flex-col overflow-hidden rounded-lg bg-white shadow-lg dark:bg-slate-900"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start justify-between border-b border-slate-200 p-4 dark:border-slate-800">
          <p className="truncate pr-4 text-sm font-medium text-slate-800 dark:text-slate-200">{file.name}</p>
          <div className="flex shrink-0 items-center gap-3">
            <a
              href={api.downloadUrl(file.id)}
              download={file.name}
              className="rounded px-2 py-1 text-xs text-blue-700 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950"
            >
              Descargar
            </a>
            <button onClick={onClose} className="text-sm text-slate-500 hover:text-slate-700 dark:hover:text-slate-300">
              Cerrar
            </button>
          </div>
        </div>

        <div className="flex-1 overflow-auto p-4">
          {kind === 'image' && (
            <img src={api.downloadUrl(file.id)} alt={file.name} className="mx-auto max-h-full max-w-full object-contain" />
          )}
          {kind === 'pdf' && <embed src={api.downloadUrl(file.id)} type="application/pdf" className="h-[75vh] w-full" />}
          {kind === 'video' && (
            <video controls className="mx-auto max-h-full max-w-full">
              <source src={api.downloadUrl(file.id)} type={file.mime_type} />
            </video>
          )}
          {kind === 'audio' && (
            <audio controls className="w-full">
              <source src={api.downloadUrl(file.id)} type={file.mime_type} />
            </audio>
          )}

          {tooLarge && (
            <p className="p-6 text-center text-sm text-slate-500 dark:text-slate-400">
              Este archivo es demasiado grande para previsualizar ({formatBytes(file.size_bytes)}). Descárgalo para verlo.
            </p>
          )}

          {needsText && !tooLarge && loading && <p className="p-6 text-center text-sm text-slate-500 dark:text-slate-400">Cargando…</p>}
          {needsText && !tooLarge && error && <p className="p-6 text-center text-sm text-red-600 dark:text-red-400">{error}</p>}

          {kind === 'markdown' && !loading && !error && text !== null && <MarkdownPreview text={text} />}
          {kind === 'code' && !loading && !error && text !== null && language && <CodePreview text={text} language={language} />}
          {kind === 'text' && !loading && !error && text !== null && (
            <pre className="whitespace-pre-wrap break-words text-sm text-slate-800 dark:text-slate-200">{text}</pre>
          )}

          {kind === 'unsupported' && (
            <p className="p-6 text-center text-sm text-slate-500 dark:text-slate-400">
              No se puede previsualizar este tipo de archivo. Descárgalo para abrirlo.
            </p>
          )}
        </div>
      </div>
    </div>
  )
}

// MarkdownPreview: marked convierte a HTML y DOMPurify lo sanitiza antes de
// tocar el DOM -- un .md puede venir de cualquiera con permiso de escritura
// en la carpeta, incluida la subida anónima (§38), así que es contenido no
// confiable ejecutándose en el navegador de quien previsualiza (§138). Los
// estilos van con selectores de hijos porque el proyecto no tiene el plugin
// de tipografía de Tailwind -- sin esto, el reset de Tailwind deja títulos y
// listas sin distinguirse del texto normal.
function MarkdownPreview({ text }: { text: string }) {
  const [html, setHtml] = useState('')

  useEffect(() => {
    let cancelled = false
    void Promise.resolve(marked.parse(text)).then((rawHtml) => {
      // FORBID_TAGS: un <style> embebido no aporta nada a una previsualización
      // de Markdown legítima y ha sido, históricamente, una de las vías más
      // repetidas de mXSS en sanitizadores de HTML -- defensa en profundidad,
      // no una corrección de un vector encontrado (revisión de seguridad,
      // ADR-040 Fase 2).
      if (!cancelled) setHtml(DOMPurify.sanitize(rawHtml, { FORBID_TAGS: ['style'] }))
    })
    return () => {
      cancelled = true
    }
  }, [text])

  return (
    <div
      className="text-sm text-slate-800 dark:text-slate-200 [&_a]:text-blue-600 [&_a]:underline dark:[&_a]:text-blue-400 [&_blockquote]:border-l-2 [&_blockquote]:border-slate-300 [&_blockquote]:pl-3 [&_blockquote]:text-slate-500 [&_code]:rounded [&_code]:bg-slate-100 [&_code]:px-1 [&_code]:py-0.5 [&_code]:text-xs dark:[&_code]:bg-slate-800 [&_h1]:mb-2 [&_h1]:text-xl [&_h1]:font-semibold [&_h2]:mb-2 [&_h2]:text-lg [&_h2]:font-semibold [&_h3]:mb-2 [&_h3]:font-semibold [&_img]:max-w-full [&_li]:mb-1 [&_ol]:mb-3 [&_ol]:list-decimal [&_ol]:pl-5 [&_p]:mb-3 [&_pre]:overflow-auto [&_pre]:rounded-md [&_pre]:bg-slate-50 [&_pre]:p-3 dark:[&_pre]:bg-slate-950 [&_table]:border-collapse [&_td]:border [&_td]:border-slate-300 [&_td]:px-2 [&_td]:py-1 [&_th]:border [&_th]:border-slate-300 [&_th]:px-2 [&_th]:py-1 [&_ul]:mb-3 [&_ul]:list-disc [&_ul]:pl-5"
      dangerouslySetInnerHTML={{ __html: html }}
    />
  )
}

// CodePreview: highlight.js recibe el texto crudo y es EL QUE genera el
// HTML (escapa primero, colorea después) -- nunca se inyecta el contenido
// del archivo tal cual, solo la salida ya segura de la propia librería.
function CodePreview({ text, language }: { text: string; language: string }) {
  const highlighted = hljs.highlight(text, { language }).value
  return (
    <pre className="overflow-auto rounded-md bg-slate-50 p-3 text-sm dark:bg-slate-950">
      <code className={`hljs language-${language}`} dangerouslySetInnerHTML={{ __html: highlighted }} />
    </pre>
  )
}

import { useState } from 'react'
import { api } from '../api/client'

// FileThumbnail (§34, ADR-041): miniatura real para imagen/vídeo/PDF en vez
// del emoji genérico -- mismos tres tipos que ThumbnailKindForMimeType en el
// backend (internal/storage/thumbnail_job.go), para no pedir un archivo que
// estructuralmente nunca puede tener miniatura. GET .../thumbnail puede
// 404/503 (desactivado en este servidor, tipo sin miniatura posible tras
// todo, o contención transitoria del semáforo/caché) -- onError cae al
// emoji, el mismo fallback que ya describía el propio plan de esta fase.
// Sin estado de carga propio: mientras el navegador resuelve la petición se
// ve el emoji, que es exactamente lo que se vería sin esta miniatura --
// nunca un parpadeo peor que no tenerla.
const THUMBNAIL_MIME_PREFIXES = ['image/', 'video/']

function hasThumbnail(mimeType: string): boolean {
  return THUMBNAIL_MIME_PREFIXES.some((prefix) => mimeType.startsWith(prefix)) || mimeType === 'application/pdf'
}

interface FileThumbnailProps {
  fileId: string
  mimeType: string
}

export default function FileThumbnail({ fileId, mimeType }: FileThumbnailProps) {
  const [failed, setFailed] = useState(false)

  if (failed || !hasThumbnail(mimeType)) {
    return <span aria-hidden>📄</span>
  }
  return (
    <img
      src={api.thumbnailUrl(fileId)}
      alt=""
      onError={() => setFailed(true)}
      className="h-6 w-6 shrink-0 rounded object-cover"
    />
  )
}

import '../../domain/entities/file_entry.dart';

/// Cómo se previsualiza un archivo -- mismos tipos y mismo orden de
/// decisión que `kindOf` en `web/src/components/PreviewDialog.tsx` (§35).
enum PreviewKind { image, pdf, video, audio, markdown, code, text, unsupported }

/// Extensiones que se muestran como código (las mismas que la web).
const _codeExtensions = {
  'sh',
  'bash',
  'zsh',
  'c',
  'h',
  'cpp',
  'cc',
  'hpp',
  'cs',
  'css',
  'scss',
  'go',
  'java',
  'js',
  'jsx',
  'mjs',
  'cjs',
  'json',
  'php',
  'py',
  'rb',
  'rs',
  'sql',
  'ts',
  'tsx',
  'html',
  'htm',
  'xml',
  'svg',
  'yml',
  'yaml',
};

/// Texto, Markdown y código se cargan enteros en memoria: por encima de
/// esto no se previsualizan (mismo límite que la web).
const int kMaxTextPreviewBytes = 5 * 1024 * 1024;

/// Imagen y PDF también se cargan en memoria (el visor los necesita
/// enteros); vídeo y audio no tienen límite porque se reproducen por partes.
const int kMaxImagePreviewBytes = 64 * 1024 * 1024;
const int kMaxPdfPreviewBytes = 100 * 1024 * 1024;

/// Megapíxeles máximos de una imagen en la vista previa (el mismo tope que
/// `thumbnails.maxPixels` del servidor por defecto).
const int kMaxImagePreviewPixels = 40000000;

String _extensionOf(String name) {
  final dot = name.lastIndexOf('.');
  return dot < 0 ? '' : name.substring(dot + 1).toLowerCase();
}

PreviewKind previewKindOf(FileEntry file) {
  final mime = file.mimeType.toLowerCase();
  final ext = _extensionOf(file.name);
  // SVG: aunque sea image/svg+xml, Flutter no lo pinta sin otra dependencia
  // y como texto es legible -- se muestra como código, igual que su
  // extensión en la web.
  if (mime.startsWith('image/') && ext != 'svg') return PreviewKind.image;
  if (mime == 'application/pdf') return PreviewKind.pdf;
  if (mime.startsWith('video/')) return PreviewKind.video;
  if (mime.startsWith('audio/')) return PreviewKind.audio;
  if (ext == 'md' || ext == 'markdown') return PreviewKind.markdown;
  if (_codeExtensions.contains(ext)) return PreviewKind.code;
  if (mime.startsWith('text/')) return PreviewKind.text;
  return PreviewKind.unsupported;
}

/// Límite de tamaño para la vista previa, o `null` si no lo tiene.
int? previewSizeLimit(PreviewKind kind) => switch (kind) {
  PreviewKind.markdown ||
  PreviewKind.code ||
  PreviewKind.text => kMaxTextPreviewBytes,
  PreviewKind.image => kMaxImagePreviewBytes,
  PreviewKind.pdf => kMaxPdfPreviewBytes,
  PreviewKind.video || PreviewKind.audio || PreviewKind.unsupported => null,
};

bool isPreviewable(FileEntry file) =>
    previewKindOf(file) != PreviewKind.unsupported;

import 'package:flutter/material.dart';

import '../theme/app_palette.dart';

/// Familia visual de un archivo, deducida del tipo MIME y, si no basta,
/// de la extensión. Solo decide icono y color: nunca se usa para lógica.
enum FileKind {
  folder,
  image,
  video,
  audio,
  pdf,
  document,
  spreadsheet,
  presentation,
  archive,
  code,
  text,
  generic,
}

const _codeExtensions = {
  'dart',
  'go',
  'js',
  'ts',
  'tsx',
  'jsx',
  'py',
  'rs',
  'java',
  'kt',
  'c',
  'h',
  'cpp',
  'cs',
  'rb',
  'php',
  'swift',
  'sh',
  'ps1',
  'bat',
  'sql',
  'html',
  'css',
  'scss',
  'json',
  'yaml',
  'yml',
  'xml',
  'toml',
};

FileKind fileKindOf(String name, {String? mimeType}) {
  final mime = (mimeType ?? '').toLowerCase();
  final dot = name.lastIndexOf('.');
  final ext = dot > 0 ? name.substring(dot + 1).toLowerCase() : '';

  if (mime.startsWith('image/')) return FileKind.image;
  if (mime.startsWith('video/')) return FileKind.video;
  if (mime.startsWith('audio/')) return FileKind.audio;
  if (mime == 'application/pdf' || ext == 'pdf') return FileKind.pdf;

  switch (ext) {
    case 'jpg' ||
        'jpeg' ||
        'png' ||
        'gif' ||
        'webp' ||
        'bmp' ||
        'svg' ||
        'heic' ||
        'tiff' ||
        'ico':
      return FileKind.image;
    case 'mp4' || 'mkv' || 'mov' || 'avi' || 'webm' || 'wmv':
      return FileKind.video;
    case 'mp3' || 'wav' || 'flac' || 'ogg' || 'm4a' || 'aac':
      return FileKind.audio;
    case 'doc' || 'docx' || 'odt' || 'rtf' || 'pages':
      return FileKind.document;
    case 'xls' || 'xlsx' || 'ods' || 'csv' || 'numbers':
      return FileKind.spreadsheet;
    case 'ppt' || 'pptx' || 'odp' || 'key':
      return FileKind.presentation;
    case 'zip' || 'rar' || '7z' || 'tar' || 'gz' || 'bz2' || 'xz' || 'iso':
      return FileKind.archive;
    case 'txt' || 'md' || 'log' || 'ini' || 'cfg' || 'conf':
      return FileKind.text;
  }
  if (_codeExtensions.contains(ext)) return FileKind.code;
  if (mime.startsWith('text/')) return FileKind.text;
  return FileKind.generic;
}

({IconData icon, Color color}) fileKindStyle(FileKind kind, AppPalette p) {
  return switch (kind) {
    FileKind.folder => (
      icon: Icons.folder_rounded,
      color: const Color(0xFF3B82F6),
    ),
    FileKind.image => (
      icon: Icons.image_outlined,
      color: const Color(0xFF8B5CF6),
    ),
    FileKind.video => (
      icon: Icons.movie_outlined,
      color: const Color(0xFFEC4899),
    ),
    FileKind.audio => (
      icon: Icons.music_note_outlined,
      color: const Color(0xFFF97316),
    ),
    FileKind.pdf => (
      icon: Icons.picture_as_pdf_outlined,
      color: const Color(0xFFEF4444),
    ),
    FileKind.document => (
      icon: Icons.description_outlined,
      color: const Color(0xFF2563EB),
    ),
    FileKind.spreadsheet => (
      icon: Icons.table_chart_outlined,
      color: const Color(0xFF16A34A),
    ),
    FileKind.presentation => (
      icon: Icons.slideshow_outlined,
      color: const Color(0xFFEA580C),
    ),
    FileKind.archive => (
      icon: Icons.inventory_2_outlined,
      color: const Color(0xFFA16207),
    ),
    FileKind.code => (icon: Icons.code_rounded, color: const Color(0xFF0891B2)),
    FileKind.text => (icon: Icons.article_outlined, color: p.textSecondary),
    FileKind.generic => (
      icon: Icons.insert_drive_file_outlined,
      color: p.textMuted,
    ),
  };
}

/// Icono de archivo/carpeta con su color. [boxed] lo dibuja dentro de un
/// cuadrado de fondo suave (para la vista de cuadrícula y las tarjetas).
class FileTypeIcon extends StatelessWidget {
  const FileTypeIcon({
    super.key,
    required this.kind,
    this.size = 20,
    this.boxed = false,
  });

  FileTypeIcon.forName(
    String name, {
    super.key,
    String? mimeType,
    bool isDirectory = false,
    this.size = 20,
    this.boxed = false,
  }) : kind = isDirectory
           ? FileKind.folder
           : fileKindOf(name, mimeType: mimeType);

  final FileKind kind;
  final double size;
  final bool boxed;

  @override
  Widget build(BuildContext context) {
    final style = fileKindStyle(kind, context.palette);
    final icon = Icon(style.icon, size: size, color: style.color);
    if (!boxed) return icon;
    return Container(
      width: size * 1.8,
      height: size * 1.8,
      alignment: Alignment.center,
      decoration: BoxDecoration(
        color: style.color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(size * 0.45),
      ),
      child: icon,
    );
  }
}

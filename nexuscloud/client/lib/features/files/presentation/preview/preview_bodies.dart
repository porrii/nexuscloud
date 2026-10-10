import 'dart:convert';
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:pdfrx/pdfrx.dart';

import '../../../../core/format/formatters.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/theme/app_theme.dart';
import '../../../../core/widgets/view_states.dart';
import '../../data/file_content_service.dart';
import '../../domain/entities/file_entry.dart';
import 'media_preview.dart';
import 'preview_kind.dart';

/// Contenido de la vista previa según el tipo. Imagen, PDF y texto se
/// descargan enteros a memoria (con límite de tamaño); vídeo y audio se
/// reproducen por partes desde el servidor.
class PreviewBody extends StatefulWidget {
  const PreviewBody({
    super.key,
    required this.file,
    required this.kind,
    required this.content,
    required this.onDownload,
  });

  final FileEntry file;
  final PreviewKind kind;
  final FileContentService content;
  final VoidCallback onDownload;

  @override
  State<PreviewBody> createState() => _PreviewBodyState();
}

class _PreviewBodyState extends State<PreviewBody> {
  Future<Uint8List>? _bytes;

  bool get _needsBytes => switch (widget.kind) {
    PreviewKind.image ||
    PreviewKind.pdf ||
    PreviewKind.markdown ||
    PreviewKind.code ||
    PreviewKind.text => true,
    _ => false,
  };

  bool get _tooLarge {
    final limit = previewSizeLimit(widget.kind);
    return limit != null && widget.file.sizeBytes > limit;
  }

  @override
  void initState() {
    super.initState();
    if (_needsBytes && !_tooLarge) {
      _bytes = _fetch();
    }
  }

  Future<Uint8List> _fetch() async {
    final bytes = await widget.content.fetchBytes(
      widget.file.id,
      maxBytes: previewSizeLimit(widget.kind),
    );
    if (widget.kind == PreviewKind.image) {
      final pixels = await _pixelCount(bytes);
      if (pixels != null && pixels > kMaxImagePreviewPixels) {
        throw const _TooManyPixels();
      }
    }
    return bytes;
  }

  /// Ancho × alto declarados en la cabecera, SIN decodificar los píxeles:
  /// una imagen de 30000×30000 en un PNG de pocos MB ocuparía 3,6 GB al
  /// decodificarse y tumbaría la app. `null` si el formato no se reconoce
  /// (entonces `Image` mostrará su propio error).
  static Future<int?> _pixelCount(Uint8List bytes) async {
    try {
      final buffer = await ui.ImmutableBuffer.fromUint8List(bytes);
      try {
        final descriptor = await ui.ImageDescriptor.encoded(buffer);
        final pixels = descriptor.width * descriptor.height;
        descriptor.dispose();
        return pixels;
      } finally {
        buffer.dispose();
      }
    } catch (_) {
      return null;
    }
  }

  void _retry() {
    setState(() {
      _bytes = _fetch();
    });
  }

  Widget _downloadButton() => FilledButton.icon(
    onPressed: widget.onDownload,
    icon: const Icon(Icons.download_rounded, size: 18),
    label: const Text('Descargar'),
  );

  @override
  Widget build(BuildContext context) {
    if (widget.kind == PreviewKind.unsupported) {
      return EmptyState(
        icon: Icons.visibility_off_outlined,
        title: 'No hay vista previa para este tipo de archivo',
        message: 'Descárgalo para abrirlo con otra aplicación.',
        action: _downloadButton(),
      );
    }
    if (_tooLarge) {
      return EmptyState(
        icon: Icons.inventory_2_outlined,
        title: 'Demasiado grande para la vista previa',
        message:
            'Ocupa ${formatBytes(widget.file.sizeBytes)} y el límite para '
            'este tipo es ${formatBytes(previewSizeLimit(widget.kind)!)}.',
        action: _downloadButton(),
      );
    }
    if (widget.kind == PreviewKind.video || widget.kind == PreviewKind.audio) {
      return MediaPreview(
        file: widget.file,
        audioOnly: widget.kind == PreviewKind.audio,
        content: widget.content,
      );
    }

    return FutureBuilder<Uint8List>(
      future: _bytes,
      builder: (context, snapshot) {
        if (snapshot.error is _TooManyPixels) {
          return EmptyState(
            icon: Icons.inventory_2_outlined,
            title: 'Imagen demasiado grande para la vista previa',
            message:
                'Tiene más de ${kMaxImagePreviewPixels ~/ 1000000} '
                'megapíxeles. Descárgala para abrirla con otra aplicación.',
            action: _downloadButton(),
          );
        }
        final failure = snapshot.error;
        if (failure is ApiException && failure.code == 'too_large') {
          return EmptyState(
            icon: Icons.inventory_2_outlined,
            title: 'Demasiado grande para la vista previa',
            message: failure.message,
            action: _downloadButton(),
          );
        }
        if (snapshot.hasError) {
          final error = snapshot.error;
          return ErrorState(
            message: error is ApiException
                ? error.message
                : 'No se pudo cargar el archivo.',
            onRetry: _retry,
          );
        }
        final bytes = snapshot.data;
        if (bytes == null) return const LoadingState();
        return switch (widget.kind) {
          PreviewKind.image => ImagePreview(bytes: bytes),
          PreviewKind.pdf => PdfPreview(file: widget.file, bytes: bytes),
          PreviewKind.markdown => MarkdownPreview(text: _decode(bytes)),
          _ => TextPreview(text: _decode(bytes)),
        };
      },
    );
  }

  static String _decode(Uint8List bytes) =>
      utf8.decode(bytes, allowMalformed: true);
}

const int _kMaxDecodedSide = 4096;

class ImagePreview extends StatelessWidget {
  const ImagePreview({super.key, required this.bytes});

  final Uint8List bytes;

  @override
  Widget build(BuildContext context) {
    return ColoredBox(
      color: context.palette.surfaceMuted,
      child: InteractiveViewer(
        maxScale: 8,
        child: Center(
          // Se decodifica como mucho a 4096 px de lado: una foto de 100 Mpx
          // a resolución completa ocuparía 400 MB de RAM solo para verla.
          child: Image(
            image: ResizeImage(
              MemoryImage(bytes),
              width: _kMaxDecodedSide,
              height: _kMaxDecodedSide,
              policy: ResizeImagePolicy.fit,
            ),
            fit: BoxFit.contain,
            filterQuality: FilterQuality.medium,
            errorBuilder: (context, error, stackTrace) => const EmptyState(
              icon: Icons.broken_image_outlined,
              title: 'No se pudo mostrar la imagen',
              message: 'El formato no es compatible o el archivo está dañado.',
            ),
          ),
        ),
      ),
    );
  }
}

class PdfPreview extends StatelessWidget {
  const PdfPreview({super.key, required this.file, required this.bytes});

  final FileEntry file;
  final Uint8List bytes;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return PdfViewer.data(
      bytes,
      sourceName: 'nexuscloud-${file.id}-${file.sha256}',
      params: PdfViewerParams(
        backgroundColor: p.surfaceMuted,
        errorBannerBuilder: (context, error, stackTrace, documentRef) =>
            const EmptyState(
              icon: Icons.picture_as_pdf_outlined,
              title: 'No se pudo abrir el PDF',
              message:
                  'Puede estar protegido con contraseña o dañado. '
                  'Descárgalo para abrirlo con otra aplicación.',
            ),
      ),
    );
  }
}

class TextPreview extends StatelessWidget {
  const TextPreview({super.key, required this.text});

  final String text;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return ColoredBox(
      color: p.surfaceMuted.withValues(alpha: 0.4),
      child: Scrollbar(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(20),
          child: SelectableText(
            text,
            style: monoTextStyle.copyWith(
              fontSize: 13,
              height: 1.45,
              color: p.textPrimary,
            ),
          ),
        ),
      ),
    );
  }
}

class MarkdownPreview extends StatelessWidget {
  const MarkdownPreview({super.key, required this.text});

  final String text;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final theme = Theme.of(context);
    return Markdown(
      data: text,
      selectable: true,
      padding: const EdgeInsets.fromLTRB(28, 20, 28, 28),
      styleSheet: MarkdownStyleSheet.fromTheme(theme).copyWith(
        code: monoTextStyle.copyWith(
          fontSize: 13,
          backgroundColor: p.surfaceMuted,
        ),
        codeblockDecoration: BoxDecoration(
          color: p.surfaceMuted,
          borderRadius: BorderRadius.circular(8),
        ),
      ),
      // Nada se carga fuera de la app: una imagen enlazada podría servir
      // para rastrear quién abre el documento (y una ruta local, para leer
      // archivos del equipo). Se muestra su texto alternativo. (La web sí
      // las carga: DOMPurify deja pasar <img>; aquí se elige lo más seguro.)
      imageBuilder: (uri, title, alt) => Text(
        '[imagen${alt == null || alt.isEmpty ? '' : ': $alt'}]',
        style: TextStyle(color: p.textMuted, fontStyle: FontStyle.italic),
      ),
    );
  }
}

class _TooManyPixels implements Exception {
  const _TooManyPixels();
}

import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../data/thumbnail_service.dart';
import '../../domain/entities/file_entry.dart';

/// Miniatura real de un archivo (§34, ADR-041) con [placeholder] (el icono
/// del tipo) mientras llega o si no la hay -- mismo criterio que
/// `web/src/components/FileThumbnail.tsx`: nunca se ve nada peor que sin
/// miniatura, ni spinners ni huecos vacíos.
///
/// Solo pide la miniatura a los tipos que el servidor sabe generar. Sin
/// [ThumbnailService] registrado (widget tests) se queda en el icono.
class FileThumbnail extends StatefulWidget {
  const FileThumbnail({
    super.key,
    required this.file,
    required this.placeholder,
    this.borderRadius = BorderRadius.zero,
    this.frameColor,
    this.service,
  });

  final FileEntry file;
  final Widget placeholder;
  final BorderRadius borderRadius;

  /// Borde fino sobre la miniatura (separa una foto clara del fondo).
  final Color? frameColor;

  /// Por defecto, el registrado en el localizador.
  final ThumbnailService? service;

  @override
  State<FileThumbnail> createState() => _FileThumbnailState();
}

class _FileThumbnailState extends State<FileThumbnail> {
  Uint8List? _bytes;

  ThumbnailService? get _service =>
      widget.service ??
      (sl.isRegistered<ThumbnailService>() ? sl<ThumbnailService>() : null);

  @override
  void initState() {
    super.initState();
    _start();
  }

  @override
  void didUpdateWidget(FileThumbnail oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.file.id != widget.file.id ||
        oldWidget.file.sha256 != widget.file.sha256) {
      _bytes = null;
      _start();
    }
  }

  bool _isCurrent(String id, String sha256) =>
      mounted && widget.file.id == id && widget.file.sha256 == sha256;

  void _start() {
    final service = _service;
    final file = widget.file;
    if (service == null || !mimeTypeHasThumbnail(file.mimeType)) return;

    // Ya descargada: se pinta en el primer frame, sin fundido.
    _bytes = service.cached(file.id, file.sha256);
    if (_bytes != null || service.isKnownMissing(file.id, file.sha256)) {
      return;
    }
    final id = file.id;
    final sha256 = file.sha256;
    service.load(id, sha256, stillWanted: () => _isCurrent(id, sha256)).then((
      bytes,
    ) {
      if (bytes == null || !_isCurrent(id, sha256)) return;
      setState(() => _bytes = bytes);
    });
  }

  @override
  Widget build(BuildContext context) {
    final bytes = _bytes;
    return AnimatedSwitcher(
      duration: const Duration(milliseconds: 180),
      child: bytes == null
          ? KeyedSubtree(
              key: const ValueKey('thumbnail-placeholder'),
              child: widget.placeholder,
            )
          : Container(
              key: ValueKey('thumbnail-${widget.file.id}'),
              clipBehavior: Clip.antiAlias,
              decoration: BoxDecoration(borderRadius: widget.borderRadius),
              foregroundDecoration: widget.frameColor == null
                  ? null
                  : BoxDecoration(
                      borderRadius: widget.borderRadius,
                      border: Border.all(color: widget.frameColor!),
                    ),
              child: Image.memory(
                bytes,
                fit: BoxFit.cover,
                // Una página se lee de arriba abajo: recortar por el centro
                // dejaba a la vista justo la parte en blanco de muchos PDF.
                alignment: widget.file.mimeType == 'application/pdf'
                    ? Alignment.topCenter
                    : Alignment.center,
                width: double.infinity,
                height: double.infinity,
                gaplessPlayback: true,
                filterQuality: FilterQuality.medium,
                // Decorativa: el nombre del archivo ya está al lado.
                excludeFromSemantics: true,
                // Bytes que no se pueden decodificar: el icono, como sin
                // miniatura.
                errorBuilder: (context, error, stackTrace) =>
                    widget.placeholder,
              ),
            ),
    );
  }
}

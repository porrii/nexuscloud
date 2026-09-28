import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/theme/app_palette.dart';
import '../../data/update_apply_service.dart';
import '../../data/update_check_service.dart';
import '../../di/update_dependencies.dart';
import '../../domain/entities/update_asset.dart';
import '../../domain/entities/update_check_result.dart';

enum _Phase {
  checking,
  upToDate,
  unavailable,
  available,
  downloading,
  readyToRestart,
  error,
}

/// Comprobar, descargar y aplicar una actualización del cliente de
/// escritorio (Velopack, ADR-032). Nunca descarga ni aplica nada sin que
/// el usuario lo pida explícitamente en cada paso -- la app se
/// auto-modifica, eso merece confirmación, no automatismo silencioso.
///
/// Es el contenido de la tarjeta "Actualizaciones" de Ajustes (antes una
/// página propia empujada desde un icono de la barra superior).
class UpdatesPanel extends StatefulWidget {
  const UpdatesPanel({super.key, this.onAvailabilityChanged});

  /// Avisa al shell para encender o apagar la marca "Nueva" de la barra
  /// lateral según el resultado de cada comprobación.
  final ValueChanged<bool>? onAvailabilityChanged;

  @override
  State<UpdatesPanel> createState() => _UpdatesPanelState();
}

class _UpdatesPanelState extends State<UpdatesPanel> {
  final UpdateCheckService _checkService = sl<UpdateCheckService>();
  final UpdateApplyService _applyService = sl<UpdateApplyService>();

  _Phase _phase = _Phase.checking;
  UpdateAsset? _available;
  String? _stagedPackagePath;
  double _downloadProgress = 0;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    _check();
  }

  Future<void> _check() async {
    setState(() => _phase = _Phase.checking);
    final result = await _checkService.check();
    if (!mounted) return;
    setState(() {
      switch (result.status) {
        case UpdateCheckStatus.upToDate:
          _phase = _Phase.upToDate;
        case UpdateCheckStatus.unavailable:
          _phase = _Phase.unavailable;
        case UpdateCheckStatus.updateAvailable:
          _phase = _Phase.available;
          _available = result.available;
      }
    });
    widget.onAvailabilityChanged?.call(
      result.status == UpdateCheckStatus.updateAvailable,
    );
  }

  Future<void> _download() async {
    final asset = _available;
    if (asset == null) return;
    setState(() {
      _phase = _Phase.downloading;
      _downloadProgress = 0;
    });
    try {
      final path = await _applyService.stageUpdate(
        asset,
        onProgress: (received, total) {
          if (!mounted || total <= 0) return;
          setState(() => _downloadProgress = received / total);
        },
      );
      if (!mounted) return;
      setState(() {
        _stagedPackagePath = path;
        _phase = _Phase.readyToRestart;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _phase = _Phase.error;
        _errorMessage = '$e';
      });
    }
  }

  Future<void> _restartAndApply() async {
    final path = _stagedPackagePath;
    if (path == null) return;
    try {
      // Si esto tiene éxito, el proceso termina aquí dentro (ver
      // UpdateApplyService.applyAndRestart) -- no hay nada que hacer
      // después de este await en el camino feliz.
      await _applyService.applyAndRestart(path);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _phase = _Phase.error;
        _errorMessage = '$e';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;

    final (
      IconData icon,
      Color color,
      String title,
      String? detail,
    ) = switch (_phase) {
      _Phase.checking => (
        Icons.sync_rounded,
        p.accent,
        'Buscando actualizaciones…',
        null,
      ),
      _Phase.upToDate => (
        Icons.check_circle_outline_rounded,
        p.success,
        'Ya tienes la última versión.',
        null,
      ),
      _Phase.unavailable => (
        Icons.cloud_off_outlined,
        p.textMuted,
        'No se pudo comprobar si hay actualizaciones.',
        'Esta instancia puede no tener activada esta función, o no hay red.',
      ),
      _Phase.available => (
        Icons.new_releases_outlined,
        p.accent,
        'Hay una versión nueva disponible: ${_available!.version}',
        'Se descarga y se verifica antes de instalarla.',
      ),
      _Phase.downloading => (
        Icons.download_rounded,
        p.accent,
        'Descargando ${_available?.version ?? ''}…',
        null,
      ),
      _Phase.readyToRestart => (
        Icons.verified_outlined,
        p.success,
        'Actualización descargada y verificada.',
        'La app se cerrará y volverá a abrirse ya actualizada.',
      ),
      _Phase.error => (
        Icons.error_outline_rounded,
        p.danger,
        'No se pudo completar la actualización.',
        _errorMessage,
      ),
    };

    final Widget action = switch (_phase) {
      _Phase.checking => const SizedBox(
        width: 20,
        height: 20,
        child: CircularProgressIndicator(strokeWidth: 2),
      ),
      _Phase.available => FilledButton.icon(
        onPressed: _download,
        icon: const Icon(Icons.download_rounded, size: 18),
        label: const Text('Descargar'),
      ),
      _Phase.downloading => const SizedBox.shrink(),
      _Phase.readyToRestart => FilledButton.icon(
        onPressed: _restartAndApply,
        icon: const Icon(Icons.restart_alt_rounded, size: 18),
        label: const Text('Reiniciar y actualizar'),
      ),
      _ => OutlinedButton(
        onPressed: _check,
        child: const Text('Buscar de nuevo'),
      ),
    };

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            Icon(icon, color: color, size: 22),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    title,
                    style: text.bodyLarge?.copyWith(
                      fontWeight: FontWeight.w500,
                    ),
                  ),
                  if (detail != null) ...[
                    const SizedBox(height: 2),
                    Text(detail, style: text.bodySmall),
                  ],
                ],
              ),
            ),
            const SizedBox(width: 12),
            action,
          ],
        ),
        if (_phase == _Phase.downloading) ...[
          const SizedBox(height: 12),
          LinearProgressIndicator(
            value: _downloadProgress > 0 ? _downloadProgress : null,
          ),
        ],
        const SizedBox(height: 12),
        Text('Versión instalada: $appVersion', style: text.bodySmall),
      ],
    );
  }
}

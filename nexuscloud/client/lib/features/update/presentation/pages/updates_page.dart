import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../data/update_apply_service.dart';
import '../../data/update_check_service.dart';
import '../../di/update_dependencies.dart';
import '../../domain/entities/update_asset.dart';
import '../../domain/entities/update_check_result.dart';

enum _Phase { checking, upToDate, unavailable, available, downloading, readyToRestart, error }

/// Comprobar, descargar y aplicar una actualización del cliente de
/// escritorio (Velopack, ADR-032). Nunca descarga ni aplica nada sin que
/// el usuario lo pida explícitamente en cada paso -- la app se
/// auto-modifica, eso merece confirmación, no automatismo silencioso.
class UpdatesPage extends StatefulWidget {
  const UpdatesPage({super.key});

  @override
  State<UpdatesPage> createState() => _UpdatesPageState();
}

class _UpdatesPageState extends State<UpdatesPage> {
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
    return Scaffold(
      appBar: AppBar(title: const Text('Actualizaciones')),
      body: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Versión instalada: $appVersion'),
            const SizedBox(height: 24),
            _buildBody(context),
          ],
        ),
      ),
    );
  }

  Widget _buildBody(BuildContext context) {
    switch (_phase) {
      case _Phase.checking:
        return const Row(
          children: [
            SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2)),
            SizedBox(width: 12),
            Text('Buscando actualizaciones...'),
          ],
        );
      case _Phase.upToDate:
        return _withRetry(
          const Text('Ya tienes la última versión.'),
        );
      case _Phase.unavailable:
        return _withRetry(
          const Text(
            'No se pudo comprobar si hay actualizaciones (esta instancia '
            'puede no tener activada esta función, o no hay red).',
          ),
        );
      case _Phase.available:
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Hay una versión nueva disponible: ${_available!.version}'),
            const SizedBox(height: 16),
            ElevatedButton(onPressed: _download, child: const Text('Descargar')),
          ],
        );
      case _Phase.downloading:
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Descargando ${_available?.version ?? ''}...'),
            const SizedBox(height: 12),
            LinearProgressIndicator(value: _downloadProgress > 0 ? _downloadProgress : null),
          ],
        );
      case _Phase.readyToRestart:
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('Actualización descargada y verificada.'),
            const SizedBox(height: 16),
            ElevatedButton(
              onPressed: _restartAndApply,
              child: const Text('Reiniciar y actualizar'),
            ),
          ],
        );
      case _Phase.error:
        return _withRetry(
          Text(_errorMessage ?? 'No se pudo completar la actualización.'),
        );
    }
  }

  Widget _withRetry(Widget message) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        message,
        const SizedBox(height: 16),
        OutlinedButton(onPressed: _check, child: const Text('Buscar de nuevo')),
      ],
    );
  }
}

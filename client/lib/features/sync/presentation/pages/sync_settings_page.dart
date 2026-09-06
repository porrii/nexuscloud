import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/network/api_exception.dart';
import '../../domain/entities/sync_pair.dart';
import '../../domain/entities/sync_result.dart';
import '../../domain/repositories/sync_config_repository.dart';
import '../../domain/services/sync_engine.dart';

/// Configura el único par carpeta-remota/carpeta-local (slice A, ADR-011)
/// y dispara una sincronización manual. Sin modo automático ni varios
/// pares en este slice.
class SyncSettingsPage extends StatefulWidget {
  const SyncSettingsPage({super.key});

  @override
  State<SyncSettingsPage> createState() => _SyncSettingsPageState();
}

class _SyncSettingsPageState extends State<SyncSettingsPage> {
  final SyncConfigRepository _configRepository = sl<SyncConfigRepository>();
  final SyncEngine _syncEngine = sl<SyncEngine>();

  final _remotePathController = TextEditingController(text: '/');
  String? _localPath;

  bool _syncing = false;
  String? _statusMessage;
  String? _startError;
  SyncResult? _lastResult;

  @override
  void initState() {
    super.initState();
    _loadSavedPair();
  }

  @override
  void dispose() {
    _remotePathController.dispose();
    super.dispose();
  }

  Future<void> _loadSavedPair() async {
    final saved = await _configRepository.read();
    if (saved == null || !mounted) return;
    setState(() {
      _remotePathController.text = saved.remotePath;
      _localPath = saved.localPath;
    });
  }

  Future<void> _pickLocalFolder() async {
    final path = await getDirectoryPath(confirmButtonText: 'Elegir carpeta');
    if (path == null || !mounted) return;
    setState(() => _localPath = path);
  }

  Future<void> _syncNow() async {
    final remotePath = _remotePathController.text.trim();
    final localPath = _localPath;
    if (remotePath.isEmpty || localPath == null) {
      setState(() {
        _startError = 'Elige una carpeta remota y una carpeta local primero.';
      });
      return;
    }

    final pair = SyncPair(remotePath: remotePath, localPath: localPath);
    await _configRepository.save(pair);

    setState(() {
      _syncing = true;
      _startError = null;
      _lastResult = null;
      _statusMessage = 'Preparando...';
    });

    try {
      final result = await _syncEngine.syncNow(
        pair,
        onStatus: (status) {
          if (!mounted) return;
          setState(() => _statusMessage = status);
        },
      );
      if (!mounted) return;
      setState(() {
        _syncing = false;
        _statusMessage = null;
        _lastResult = result;
      });
    } on ApiException catch (e) {
      // Fallo de arranque (p.ej. la carpeta remota configurada ya no
      // existe) -- distinto de los errores por archivo, que van dentro
      // de SyncResult.errors sin abortar el resto.
      if (!mounted) return;
      setState(() {
        _syncing = false;
        _statusMessage = null;
        _startError = e.message;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Sincronización')),
      body: Padding(
        padding: const EdgeInsets.all(24),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 520),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(
                'Sincroniza una carpeta remota hacia una carpeta local. '
                'Por ahora es de un solo sentido (servidor → local) y '
                'manual -- no borra ni sube nada, solo trae lo nuevo o '
                'cambiado.',
                style: Theme.of(context).textTheme.bodyMedium,
              ),
              const SizedBox(height: 24),
              TextField(
                controller: _remotePathController,
                enabled: !_syncing,
                decoration: const InputDecoration(
                  labelText: 'Carpeta remota',
                  hintText: '/',
                ),
              ),
              const SizedBox(height: 16),
              Row(
                children: [
                  Expanded(
                    child: Text(
                      _localPath ?? 'Ninguna carpeta local elegida',
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  const SizedBox(width: 12),
                  OutlinedButton(
                    onPressed: _syncing ? null : _pickLocalFolder,
                    child: const Text('Elegir carpeta local'),
                  ),
                ],
              ),
              const SizedBox(height: 24),
              FilledButton(
                onPressed: _syncing ? null : _syncNow,
                child: _syncing
                    ? const SizedBox(
                        width: 20,
                        height: 20,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Text('Sincronizar ahora'),
              ),
              if (_syncing && _statusMessage != null) ...[
                const SizedBox(height: 16),
                Text(_statusMessage!, style: Theme.of(context).textTheme.bodySmall),
              ],
              if (_startError != null) ...[
                const SizedBox(height: 16),
                Text(
                  _startError!,
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              ],
              if (_lastResult != null) ...[
                const SizedBox(height: 24),
                const Divider(),
                const SizedBox(height: 8),
                Text(
                  'Última sincronización: ${_lastResult!.finishedAt.toLocal()}\n'
                  '${_lastResult!.downloaded} descargados, '
                  '${_lastResult!.skipped} ya al día, '
                  '${_lastResult!.errors.length} errores',
                ),
                if (_lastResult!.errors.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  ConstrainedBox(
                    constraints: const BoxConstraints(maxHeight: 200),
                    child: ListView(
                      shrinkWrap: true,
                      children: [
                        for (final error in _lastResult!.errors)
                          Text(
                            '• $error',
                            style: TextStyle(
                              color: Theme.of(context).colorScheme.error,
                            ),
                          ),
                      ],
                    ),
                  ),
                ],
              ],
            ],
          ),
        ),
      ),
    );
  }
}

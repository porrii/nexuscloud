import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/widgets/confirm_dialog.dart';
import '../../domain/entities/local_trash_entry.dart';
import '../../domain/entities/sync_pair_config.dart';
import '../../domain/repositories/local_trash_store.dart';
import '../../domain/repositories/sync_config_repository.dart';

enum _LoadState { loading, loaded, error }

/// Vista de lo que ADR-013 movió a la papelera local (slice 16) -- hasta
/// ahora `LocalTrashStore` solo se podía escribir (`moveToTrash`), sin
/// forma de ver ni recuperar nada desde la app. Mismo criterio que
/// `TrashPage` (papelera del servidor): "Restaurar" sin confirmación
/// (reversible), "Eliminar para siempre" con confirmación (irreversible),
/// recarga completa tras cualquier acción con éxito.
class LocalTrashPage extends StatefulWidget {
  const LocalTrashPage({super.key});

  @override
  State<LocalTrashPage> createState() => _LocalTrashPageState();
}

class _LocalTrashPageState extends State<LocalTrashPage> {
  final LocalTrashStore _trashStore = sl<LocalTrashStore>();
  final SyncConfigRepository _configRepository = sl<SyncConfigRepository>();

  List<LocalTrashEntry>? _entries;
  Map<String, SyncPairConfig> _pairsByKey = {};
  _LoadState _state = _LoadState.loading;
  String? _errorMessage;

  /// Errores de una acción puntual (p.ej. `LocalTrashRestoreConflict`),
  /// aislados a SU fila -- igual que los errores de descarga por versión en
  /// `FileVersionsPage`, nunca tiran toda la página a un estado de error.
  final Map<String, String> _rowErrors = {};
  final Set<String> _busyPaths = {};

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _state = _LoadState.loading;
      _rowErrors.clear();
    });
    try {
      final entries = await _trashStore.listAll();
      final pairs = await _configRepository.readPairs();
      entries.sort((a, b) => b.deletedAt.compareTo(a.deletedAt));
      if (!mounted) return;
      setState(() {
        _entries = entries;
        _pairsByKey = {for (final cfg in pairs) cfg.pair.stableKey: cfg};
        _state = _LoadState.loaded;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _errorMessage = 'No se pudo leer la papelera local.';
        _state = _LoadState.error;
      });
    }
  }

  Future<void> _restore(LocalTrashEntry entry) async {
    final config = _pairsByKey[entry.pairKey];
    if (config == null) return;
    setState(() {
      _busyPaths.add(entry.absolutePath);
      _rowErrors.remove(entry.absolutePath);
    });
    try {
      await _trashStore.restore(
        entry: entry,
        destinationLocalPath: config.pair.localPath,
      );
      if (mounted) await _load();
    } on LocalTrashRestoreConflict catch (e) {
      if (!mounted) return;
      setState(() {
        _busyPaths.remove(entry.absolutePath);
        _rowErrors[entry.absolutePath] = e.message;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _busyPaths.remove(entry.absolutePath);
        _rowErrors[entry.absolutePath] = 'No se pudo restaurar el archivo.';
      });
    }
  }

  Future<void> _deleteForever(LocalTrashEntry entry) async {
    final confirmed = await showConfirmDialog(
      context,
      title: 'Eliminar para siempre',
      message: '"${entry.displayPath}" se eliminará definitivamente de la '
          'papelera local y no podrá recuperarse. ¿Continuar?',
      confirmLabel: 'Eliminar para siempre',
      danger: true,
    );
    if (!confirmed || !mounted) return;

    setState(() {
      _busyPaths.add(entry.absolutePath);
      _rowErrors.remove(entry.absolutePath);
    });
    try {
      await _trashStore.deleteForever(entry);
      if (mounted) await _load();
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _busyPaths.remove(entry.absolutePath);
        _rowErrors[entry.absolutePath] = 'No se pudo eliminar el archivo.';
      });
    }
  }

  String _formatDate(DateTime dateTime) {
    final local = dateTime.toLocal();
    String two(int n) => n.toString().padLeft(2, '0');
    return '${local.year}-${two(local.month)}-${two(local.day)} '
        '${two(local.hour)}:${two(local.minute)}';
  }

  String _formatSize(int bytes) {
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    var size = bytes.toDouble();
    var unitIndex = 0;
    while (size >= 1024 && unitIndex < units.length - 1) {
      size /= 1024;
      unitIndex++;
    }
    final decimals = (unitIndex == 0 || size >= 10) ? 0 : 1;
    return '${size.toStringAsFixed(decimals)} ${units[unitIndex]}';
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Papelera local'),
        actions: [
          IconButton(
            tooltip: 'Actualizar',
            icon: const Icon(Icons.refresh),
            onPressed: _load,
          ),
        ],
      ),
      body: _buildBody(),
    );
  }

  Widget _buildBody() {
    switch (_state) {
      case _LoadState.loading:
        return const Center(child: CircularProgressIndicator());
      case _LoadState.error:
        return Center(
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(_errorMessage ?? 'No se pudo completar la operación.'),
                const SizedBox(height: 12),
                OutlinedButton(onPressed: _load, child: const Text('Reintentar')),
              ],
            ),
          ),
        );
      case _LoadState.loaded:
        final entries = _entries!;
        if (entries.isEmpty) {
          return const Center(child: Text('La papelera local está vacía'));
        }
        return ListView(
          children: [
            for (final entry in entries)
              _LocalTrashEntryRow(
                entry: entry,
                config: _pairsByKey[entry.pairKey],
                busy: _busyPaths.contains(entry.absolutePath),
                errorMessage: _rowErrors[entry.absolutePath],
                formatDate: _formatDate,
                formatSize: _formatSize,
                onRestore: () => _restore(entry),
                onDeleteForever: () => _deleteForever(entry),
              ),
          ],
        );
    }
  }
}

class _LocalTrashEntryRow extends StatelessWidget {
  const _LocalTrashEntryRow({
    required this.entry,
    required this.config,
    required this.busy,
    required this.errorMessage,
    required this.formatDate,
    required this.formatSize,
    required this.onRestore,
    required this.onDeleteForever,
  });

  final LocalTrashEntry entry;
  final SyncPairConfig? config;
  final bool busy;
  final String? errorMessage;
  final String Function(DateTime) formatDate;
  final String Function(int) formatSize;
  final VoidCallback onRestore;
  final VoidCallback onDeleteForever;

  @override
  Widget build(BuildContext context) {
    final folderLine = config != null
        ? 'Carpeta: ${config!.pair.remotePath} → ${config!.pair.localPath}'
        : 'La carpeta configurada para este archivo ya no existe en la '
            'lista de Sincronización.';

    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Padding(
            padding: EdgeInsets.only(top: 4),
            child: Icon(Icons.insert_drive_file),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(entry.relativeSegments.last, overflow: TextOverflow.ellipsis),
                Text(folderLine, style: Theme.of(context).textTheme.bodySmall),
                Text('Ruta: ${entry.displayPath}', style: Theme.of(context).textTheme.bodySmall),
                Text(
                  'Eliminado: ${formatDate(entry.deletedAt)} · ${formatSize(entry.sizeBytes)}',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
                if (errorMessage != null)
                  Text(
                    errorMessage!,
                    style: TextStyle(color: Theme.of(context).colorScheme.error),
                  ),
              ],
            ),
          ),
          IconButton(
            tooltip: config != null
                ? 'Restaurar'
                : 'No se puede restaurar: la carpeta ya no está configurada',
            icon: const Icon(Icons.restore),
            onPressed: (config != null && !busy) ? onRestore : null,
          ),
          IconButton(
            tooltip: 'Eliminar para siempre',
            icon: const Icon(Icons.delete_forever),
            onPressed: busy ? null : onDeleteForever,
          ),
        ],
      ),
    );
  }
}

import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/format/formatters.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/widgets/confirm_dialog.dart';
import '../../../../core/widgets/page_scaffold.dart';
import '../../../../core/widgets/view_states.dart';
import '../../../files/presentation/pages/trash_page.dart' show TrashRow;
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

  @override
  Widget build(BuildContext context) {
    final entries = _entries;
    return Scaffold(
      body: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          SubToolbar(
            leading: Text(
              _state == _LoadState.loaded && entries != null
                  ? '${pluralize(entries.length, 'archivo')} apartados por la sincronización'
                  : 'Papelera local',
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(color: context.palette.textSecondary),
            ),
            actions: [
              IconButton(
                tooltip: 'Actualizar',
                icon: const Icon(Icons.refresh_rounded),
                onPressed: _load,
              ),
            ],
          ),
          Expanded(child: _buildBody()),
        ],
      ),
    );
  }

  Widget _buildBody() {
    switch (_state) {
      case _LoadState.loading:
        return const LoadingState();
      case _LoadState.error:
        return ErrorState(
          message: _errorMessage ?? 'No se pudo completar la operación.',
          onRetry: _load,
        );
      case _LoadState.loaded:
        final entries = _entries!;
        if (entries.isEmpty) {
          return const EmptyState(
            icon: Icons.restore_from_trash_outlined,
            title: 'La papelera local está vacía',
            message: 'Cuando la sincronización borra un archivo de tu equipo, '
                'lo aparta aquí en vez de eliminarlo.',
          );
        }
        return ListView(
          padding: const EdgeInsets.symmetric(vertical: 8),
          children: [
            for (final entry in entries) _buildRow(entry),
          ],
        );
    }
  }

  Widget _buildRow(LocalTrashEntry entry) {
    final config = _pairsByKey[entry.pairKey];
    final busy = _busyPaths.contains(entry.absolutePath);
    final folderLine = config != null
        ? 'Carpeta: ${config.pair.remotePath} → ${config.pair.localPath}'
        : 'La carpeta configurada para este archivo ya no existe en la '
            'lista de Sincronización.';
    return TrashRow(
      name: entry.relativeSegments.last,
      subtitle: 'Ruta: ${entry.displayPath} · Eliminado: ${formatDateTime(entry.deletedAt)}',
      extraLine: folderLine,
      trailingInfo: formatBytes(entry.sizeBytes),
      errorMessage: _rowErrors[entry.absolutePath],
      busy: busy,
      restoreTooltip: config != null
          ? 'Restaurar'
          : 'No se puede restaurar: la carpeta ya no está configurada',
      onRestore: config != null ? () => _restore(entry) : null,
      onDeleteForever: () => _deleteForever(entry),
    );
  }
}

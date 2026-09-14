import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';
import 'package:path/path.dart' as p;

import '../../domain/entities/sync_direction.dart';
import '../../domain/entities/sync_pair.dart';
import '../../domain/entities/sync_pair_config.dart';

/// Diálogo para añadir o editar UN par carpeta-remota/carpeta-local
/// (slice 15). Sin [initial] crea uno nuevo; con [initial] precarga sus
/// valores y lo que se guarde SUSTITUYE a ese par en la lista de
/// `SyncSettingsPage` (misma identidad -- ver la página, que empareja por
/// posición en la lista, no por `stableKey`, así que cambiar las rutas al
/// editar es válido y no "pierde" nada -- el manifiesto/papelera del par
/// viejo simplemente queda huérfano, igual que ya pasaba en slices
/// anteriores al reconfigurar el único par que existía entonces).
///
/// Devuelve el `SyncPairConfig` resultante por `Navigator.pop`, o `null` si
/// se cancela.
Future<SyncPairConfig?> showPairEditDialog(
  BuildContext context, {
  required List<SyncPairConfig> existingPairs,
  SyncPairConfig? initial,
}) {
  return showDialog<SyncPairConfig>(
    context: context,
    builder: (context) => _PairEditDialog(
      initial: initial,
      existingPairs: existingPairs,
    ),
  );
}

class _PairEditDialog extends StatefulWidget {
  const _PairEditDialog({required this.existingPairs, this.initial});

  final SyncPairConfig? initial;

  /// Pares ya configurados contra los que validar solapamiento (#25) --
  /// quien llama a [showPairEditDialog] es responsable de excluir de esta
  /// lista el propio [initial] si se está editando (ver `SyncSettingsPage`),
  /// para no bloquear guardar sin cambios reales.
  final List<SyncPairConfig> existingPairs;

  @override
  State<_PairEditDialog> createState() => _PairEditDialogState();
}

class _PairEditDialogState extends State<_PairEditDialog> {
  late final TextEditingController _remotePathController;
  String? _localPath;
  late SyncDirection _direction;
  late bool _autoSyncEnabled;

  @override
  void initState() {
    super.initState();
    final initial = widget.initial;
    _remotePathController = TextEditingController(text: initial?.pair.remotePath ?? '/');
    _localPath = initial?.pair.localPath;
    _direction = initial?.direction ?? SyncDirection.download;
    _autoSyncEnabled = initial?.autoSyncEnabled ?? true;
  }

  @override
  void dispose() {
    _remotePathController.dispose();
    super.dispose();
  }

  Future<void> _pickLocalFolder() async {
    final path = await getDirectoryPath(confirmButtonText: 'Elegir carpeta');
    if (path == null || !mounted) return;
    setState(() => _localPath = path);
  }

  /// `null` si [localPath]/[remotePath] no solapan con ningún par de
  /// [_PairEditDialog.existingPairs]; si no, un mensaje señalando cuál y
  /// por qué lado. Dos pares con rutas solapadas sincronizarían el mismo
  /// contenido dos veces con manifiestos independientes -- conflictos y
  /// borrados cruzados impredecibles (#25).
  ///
  /// Local: `package:path` por defecto (`p`), que usa el separador de la
  /// plataforma del cliente. Remoto: `p.posix` -- `remotePath` es un
  /// `String` posix suelto (`SyncPair.remotePath`), siempre con '/' sin
  /// importar la plataforma, así que necesita su propio `Context` fijo en
  /// vez del de la plataforma.
  String? _overlapMessage(String localPath, String remotePath) {
    for (final other in widget.existingPairs) {
      final localOverlap = p.equals(other.pair.localPath, localPath) ||
          p.isWithin(other.pair.localPath, localPath) ||
          p.isWithin(localPath, other.pair.localPath);
      final remoteOverlap = p.posix.equals(other.pair.remotePath, remotePath) ||
          p.posix.isWithin(other.pair.remotePath, remotePath) ||
          p.posix.isWithin(remotePath, other.pair.remotePath);
      if (localOverlap || remoteOverlap) {
        final side = localOverlap ? 'la carpeta local' : 'la carpeta remota';
        return 'Solapa con ${other.pair.remotePath} → '
            '${other.pair.localPath} en $side.';
      }
    }
    return null;
  }

  void _save() {
    final remotePath = _remotePathController.text.trim();
    final localPath = _localPath;
    if (remotePath.isEmpty || localPath == null) return;
    Navigator.of(context).pop(
      SyncPairConfig(
        pair: SyncPair(remotePath: remotePath, localPath: localPath),
        direction: _direction,
        autoSyncEnabled: _autoSyncEnabled,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final remotePath = _remotePathController.text.trim();
    final localPath = _localPath;
    final overlapMessage = (remotePath.isNotEmpty && localPath != null)
        ? _overlapMessage(localPath, remotePath)
        : null;
    final canSave =
        remotePath.isNotEmpty && localPath != null && overlapMessage == null;
    return AlertDialog(
      title: Text(widget.initial == null ? 'Añadir carpeta a sincronizar' : 'Editar par'),
      content: SizedBox(
        width: 420,
        // `SingleChildScrollView`, no la `Column` a secas: con el
        // interruptor de auto-sync (#24) y el aviso de solapamiento (#25)
        // sumados a lo que ya había, el contenido puede superar la altura
        // que `AlertDialog` le asigna en pantallas más pequeñas -- mismo
        // criterio que `SyncSettingsPage`.
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
            TextField(
              controller: _remotePathController,
              decoration: const InputDecoration(labelText: 'Carpeta remota', hintText: '/'),
              onChanged: (_) => setState(() {}),
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
                  onPressed: _pickLocalFolder,
                  child: const Text('Elegir carpeta local'),
                ),
              ],
            ),
            const SizedBox(height: 16),
            Align(
              alignment: Alignment.centerLeft,
              child: SegmentedButton<SyncDirection>(
                segments: [
                  for (final d in SyncDirection.values)
                    ButtonSegment(value: d, label: Text(d.label)),
                ],
                selected: {_direction},
                onSelectionChanged: (selection) =>
                    setState(() => _direction = selection.first),
              ),
            ),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('Incluir en la sincronización automática'),
              subtitle: const Text(
                'Si lo desactivas, este par solo se sincroniza al pulsar '
                'un botón -- el reloj automático lo salta.',
              ),
              value: _autoSyncEnabled,
              onChanged: (value) => setState(() => _autoSyncEnabled = value),
            ),
            if (overlapMessage != null) ...[
              const SizedBox(height: 8),
              Text(
                overlapMessage,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ],
          ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancelar'),
        ),
        FilledButton(
          onPressed: canSave ? _save : null,
          child: const Text('Guardar'),
        ),
      ],
    );
  }
}

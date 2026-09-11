import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';

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
  SyncPairConfig? initial,
}) {
  return showDialog<SyncPairConfig>(
    context: context,
    builder: (context) => _PairEditDialog(initial: initial),
  );
}

class _PairEditDialog extends StatefulWidget {
  const _PairEditDialog({this.initial});

  final SyncPairConfig? initial;

  @override
  State<_PairEditDialog> createState() => _PairEditDialogState();
}

class _PairEditDialogState extends State<_PairEditDialog> {
  late final TextEditingController _remotePathController;
  String? _localPath;
  late SyncDirection _direction;

  @override
  void initState() {
    super.initState();
    final initial = widget.initial;
    _remotePathController = TextEditingController(text: initial?.pair.remotePath ?? '/');
    _localPath = initial?.pair.localPath;
    _direction = initial?.direction ?? SyncDirection.download;
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

  void _save() {
    final remotePath = _remotePathController.text.trim();
    final localPath = _localPath;
    if (remotePath.isEmpty || localPath == null) return;
    Navigator.of(context).pop(
      SyncPairConfig(
        pair: SyncPair(remotePath: remotePath, localPath: localPath),
        direction: _direction,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final canSave = _remotePathController.text.trim().isNotEmpty && _localPath != null;
    return AlertDialog(
      title: Text(widget.initial == null ? 'Añadir carpeta a sincronizar' : 'Editar par'),
      content: SizedBox(
        width: 420,
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
          ],
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

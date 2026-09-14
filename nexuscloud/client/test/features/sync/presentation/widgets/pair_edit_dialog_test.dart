import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_direction.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_pair.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_pair_config.dart';
import 'package:nexuscloud_client/features/sync/presentation/widgets/pair_edit_dialog.dart';

/// Todos los escenarios abren el diálogo en modo EDICIÓN (`initial` con una
/// carpeta local ya elegida) para no depender del selector de carpetas
/// nativo de `file_selector` (`getDirectoryPath`), que abre un diálogo del
/// SO y no es mockeable sin un canal de plataforma dedicado. El interruptor
/// de auto-sync (#24) y la validación de solapamiento (#25) no dependen de
/// ese selector -- cambiar la ruta remota (un `TextField` normal) y
/// arrancar ya con una ruta local puesta basta para ejercitarlos.
class _Harness {
  SyncPairConfig? result;
}

Future<_Harness> _openDialog(
  WidgetTester tester, {
  required SyncPairConfig initial,
  required List<SyncPairConfig> existingPairs,
}) async {
  final harness = _Harness();
  await tester.pumpWidget(
    MaterialApp(
      home: Builder(
        builder: (context) => ElevatedButton(
          onPressed: () async {
            harness.result = await showPairEditDialog(
              context,
              initial: initial,
              existingPairs: existingPairs,
            );
          },
          child: const Text('abrir'),
        ),
      ),
    ),
  );
  await tester.tap(find.text('abrir'));
  await tester.pumpAndSettle();
  return harness;
}

void main() {
  const initial = SyncPairConfig(
    pair: SyncPair(remotePath: '/Documentos', localPath: r'C:\Users\ivan\Documentos'),
    direction: SyncDirection.both,
  );

  testWidgets(
    'el interruptor de auto-sync (#24) se refleja en el SyncPairConfig devuelto',
    (tester) async {
      final harness = await _openDialog(tester, initial: initial, existingPairs: const []);

      await tester.tap(find.widgetWithText(SwitchListTile, 'Incluir en la sincronización automática'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Guardar'));
      await tester.pumpAndSettle();

      expect(harness.result, isNotNull);
      expect(harness.result!.autoSyncEnabled, isFalse);
    },
  );

  testWidgets(
    'una ruta local solapada con existingPairs (#25) bloquea el guardado y muestra el aviso',
    (tester) async {
      final overlapping = SyncPairConfig(
        pair: SyncPair(
          remotePath: '/Otra',
          localPath: r'C:\Users\ivan\Documentos\Sub',
        ),
        direction: SyncDirection.download,
      );
      await _openDialog(tester, initial: initial, existingPairs: [overlapping]);

      expect(find.textContaining('Solapa con'), findsOneWidget);
      final button = tester.widget<FilledButton>(find.widgetWithText(FilledButton, 'Guardar'));
      expect(button.onPressed, isNull);
    },
  );

  testWidgets(
    'una ruta remota solapada con existingPairs (#25) bloquea el guardado y muestra el aviso',
    (tester) async {
      final overlapping = SyncPairConfig(
        pair: SyncPair(remotePath: '/Documentos/Sub', localPath: r'C:\otra\carpeta'),
        direction: SyncDirection.download,
      );
      await _openDialog(tester, initial: initial, existingPairs: [overlapping]);

      expect(find.textContaining('Solapa con'), findsOneWidget);
      final button = tester.widget<FilledButton>(find.widgetWithText(FilledButton, 'Guardar'));
      expect(button.onPressed, isNull);
    },
  );

  testWidgets(
    'sin ningún solapamiento, guarda con normalidad',
    (tester) async {
      const other = SyncPairConfig(
        pair: SyncPair(remotePath: '/Otra', localPath: r'C:\Users\ivan\Otra'),
        direction: SyncDirection.download,
      );
      final harness = await _openDialog(tester, initial: initial, existingPairs: const [other]);

      expect(find.textContaining('Solapa con'), findsNothing);

      await tester.tap(find.text('Guardar'));
      await tester.pumpAndSettle();

      expect(harness.result, initial);
    },
  );
}

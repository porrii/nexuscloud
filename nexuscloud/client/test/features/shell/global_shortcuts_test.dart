import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/features/shell/presentation/widgets/global_shortcuts.dart';

void main() {
  const ctrl1 = SingleActivator(LogicalKeyboardKey.digit1, control: true);
  const ctrl3 = SingleActivator(LogicalKeyboardKey.digit3, control: true);

  Future<void> pressCtrl(WidgetTester tester, LogicalKeyboardKey key) async {
    await tester.sendKeyDownEvent(LogicalKeyboardKey.controlLeft);
    await tester.sendKeyEvent(key);
    await tester.sendKeyUpEvent(LogicalKeyboardKey.controlLeft);
    await tester.pump();
  }

  testWidgets('funciona aunque el foco no esté dentro (el fallo de Ctrl+1…5)', (tester) async {
    final fired = <String>[];
    // Un campo FUERA de GlobalShortcuts tiene el foco: con CallbackShortcuts
    // estas teclas nunca habrían llegado.
    await tester.pumpWidget(MaterialApp(
      home: Material(child: Column(
        children: [
          const TextField(autofocus: true),
          GlobalShortcuts(
            bindings: {
              ctrl1: () => fired.add('1'),
              ctrl3: () => fired.add('3'),
            },
            child: const SizedBox(height: 10),
          ),
        ],
      )),
    ));
    await tester.pump();

    await pressCtrl(tester, LogicalKeyboardKey.digit3);
    await pressCtrl(tester, LogicalKeyboardKey.digit1);
    // Sin foco en ningún sitio, tampoco se pierden.
    FocusManager.instance.primaryFocus?.unfocus();
    await tester.pump();
    await pressCtrl(tester, LogicalKeyboardKey.digit3);

    expect(fired, ['3', '1', '3']);
  });

  testWidgets('no actúa con un diálogo abierto encima', (tester) async {
    final fired = <String>[];
    await tester.pumpWidget(MaterialApp(
      home: GlobalShortcuts(
        bindings: {ctrl1: () => fired.add('1')},
        child: Builder(
          builder: (context) => TextButton(
            onPressed: () => showDialog<void>(
              context: context,
              builder: (_) => const AlertDialog(content: Text('Diálogo')),
            ),
            child: const Text('Abrir'),
          ),
        ),
      ),
    ));

    await tester.tap(find.text('Abrir'));
    await tester.pumpAndSettle();
    await pressCtrl(tester, LogicalKeyboardKey.digit1);
    expect(fired, isEmpty);

    Navigator.of(tester.element(find.text('Diálogo'))).pop();
    await tester.pumpAndSettle();
    await pressCtrl(tester, LogicalKeyboardKey.digit1);
    expect(fired, ['1']);
  });
}

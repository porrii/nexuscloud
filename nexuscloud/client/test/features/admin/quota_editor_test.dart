import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/features/admin/presentation/widgets/quota_editor.dart';

void main() {
  testWidgets('una cuota no redonda se conserva exacta si no se edita el número', (tester) async {
    const exact = 1234567890; // se muestra como «1.15» GB
    final emitted = <int?>[];
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: QuotaEditor(
          initial: exact,
          inheritLabel: 'Heredar',
          onChanged: emitted.add,
        ),
      ),
    ));

    // Ir a «Ilimitada» y volver a «Límite» sin tocar el número.
    await tester.tap(find.text('Ilimitada'));
    await tester.pump();
    await tester.tap(find.text('Límite'));
    await tester.pump();
    expect(emitted.last, exact);

    // Si se escribe un número nuevo, ese es el que vale.
    await tester.enterText(find.byType(TextField), '2');
    expect(emitted.last, 2 * 1024 * 1024 * 1024);
  });
}

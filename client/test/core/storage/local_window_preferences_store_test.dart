import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/storage/local_window_preferences_store.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  test('readMinimizeToTrayOnClose devuelve false si no hay nada guardado', () async {
    final store = LocalWindowPreferencesStore();

    expect(await store.readMinimizeToTrayOnClose(), isFalse);
  });

  test('saveMinimizeToTrayOnClose y readMinimizeToTrayOnClose devuelven lo mismo', () async {
    final store = LocalWindowPreferencesStore();

    await store.saveMinimizeToTrayOnClose(true);

    expect(await store.readMinimizeToTrayOnClose(), isTrue);
  });

  test('guardar false tras haber guardado true se refleja al releer', () async {
    final store = LocalWindowPreferencesStore();
    await store.saveMinimizeToTrayOnClose(true);

    await store.saveMinimizeToTrayOnClose(false);

    expect(await store.readMinimizeToTrayOnClose(), isFalse);
  });
}

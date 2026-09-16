import '../../../core/network/api_client.dart';
import '../../../core/network/api_exception.dart';
import '../domain/entities/update_asset.dart';
import '../domain/entities/update_check_result.dart';

/// Comprueba si hay una versión más reciente del cliente publicada,
/// hablando solo con el propio servidor NexusCloud -- nunca con GitHub
/// directamente (ADR-032). HTTP+JSON puro: Velopack no exige SDK ni FFI
/// para esta parte, solo para "aplicar" (ver UpdateApplyService).
class UpdateCheckService {
  UpdateCheckService({
    required ApiClient apiClient,
    required String currentVersion,
  })  : _apiClient = apiClient,
        _currentVersion = currentVersion;

  final ApiClient _apiClient;
  final String _currentVersion;

  /// Nunca lanza: una comprobación de actualizaciones es una capacidad
  /// opcional del servidor (§ opt-in, clientUpdates.enabled), así que un
  /// 404 (desactivada) o un fallo de red se traducen en
  /// [UpdateCheckStatus.unavailable], no en una excepción que la UI tendría
  /// que tratar como un error real.
  Future<UpdateCheckResult> check() async {
    final Map<String, dynamic> body;
    try {
      final response = await _apiClient.request(
        (dio) => dio.get<Map<String, dynamic>>('/public/client-updates/releases.json'),
      );
      body = response.data ?? const {};
    } on ApiException {
      return UpdateCheckResult.unavailable(_currentVersion);
    }

    final assets = (body['Assets'] as List? ?? const [])
        .cast<Map<String, dynamic>>()
        .map(UpdateAsset.fromJson)
        .where((a) => a.isFull);
    if (assets.isEmpty) {
      return UpdateCheckResult.unavailable(_currentVersion);
    }
    final full = assets.first;

    if (_isNewer(full.version, _currentVersion)) {
      return UpdateCheckResult.available(_currentVersion, full);
    }
    return UpdateCheckResult.upToDate(_currentVersion);
  }

  /// Compara "X.Y.Z" numéricamente (no como texto: "1.10.0" > "1.9.0",
  /// que una comparación de String ordenaría al revés). Deliberadamente
  /// simple -- la versión de este proyecto es siempre X.Y.Z sin sufijos de
  /// pre-release (mismo formato que exige package-client-windows.ps1 al
  /// leer pubspec.yaml), así que no hace falta un parser de semver
  /// completo para esto.
  bool _isNewer(String candidate, String current) {
    final c = _parseVersion(candidate);
    final b = _parseVersion(current);
    if (c == null || b == null) return false;
    for (var i = 0; i < 3; i++) {
      if (c[i] != b[i]) return c[i] > b[i];
    }
    return false;
  }

  List<int>? _parseVersion(String v) {
    final parts = v.split('.');
    if (parts.length != 3) return null;
    final nums = parts.map(int.tryParse).toList();
    if (nums.any((n) => n == null)) return null;
    return nums.cast<int>();
  }
}

import 'update_asset.dart';

enum UpdateCheckStatus {
  /// La versión instalada ya es la más reciente publicada.
  upToDate,

  /// Hay una versión más reciente -- [UpdateCheckResult.available] trae el
  /// asset "Full" a descargar.
  updateAvailable,

  /// No se pudo comprobar: esta instancia de NexusCloud no tiene
  /// `clientUpdates.enabled=true` (lo normal, es opt-in), o hubo un error
  /// de red. Nunca se trata como un fallo alarmante en la UI -- la
  /// comprobación de actualizaciones es una capacidad opcional del
  /// servidor, no algo que todo el mundo tenga activado.
  unavailable,
}

class UpdateCheckResult {
  const UpdateCheckResult._(this.status, this.currentVersion, this.available);

  factory UpdateCheckResult.upToDate(String currentVersion) =>
      UpdateCheckResult._(UpdateCheckStatus.upToDate, currentVersion, null);

  factory UpdateCheckResult.available(
    String currentVersion,
    UpdateAsset asset,
  ) =>
      UpdateCheckResult._(UpdateCheckStatus.updateAvailable, currentVersion, asset);

  factory UpdateCheckResult.unavailable(String currentVersion) =>
      UpdateCheckResult._(UpdateCheckStatus.unavailable, currentVersion, null);

  final UpdateCheckStatus status;
  final String currentVersion;

  /// Solo no-null cuando [status] es [UpdateCheckStatus.updateAvailable].
  final UpdateAsset? available;
}

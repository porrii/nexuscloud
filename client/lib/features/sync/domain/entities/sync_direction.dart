/// Sentido en el que el motor de sync mueve los cambios (slice 13).
///
///  - [download]: servidor → local, y nada más. Es el comportamiento
///    original (slices 3 y 8, ADR-011) y el valor por defecto -- una
///    configuración anterior a este slice se lee como [download] sin que el
///    usuario tenga que tocar nada.
///  - [upload]: local → servidor, y nada más. Nunca descarga ni borra; el
///    servidor versiona solo cualquier sobrescritura (ADR-007), así que no
///    hay riesgo de pérdida silenciosa que gestionar aquí.
///  - [both]: reconcilia los dos lados en un único recorrido, con un
///    manifiesto de estado local como base (ADR-012). Un archivo cambiado
///    en los dos lados desde la última sincronización genera una *conflict
///    copy* (§40), nunca se sobrescribe en silencio.
///
/// Ninguno de los tres modos propaga borrados en este slice (ADR-012).
enum SyncDirection {
  download,
  upload,
  both;

  /// Valor persistido en `shared_preferences`. Estable e independiente del
  /// nombre del enum -- renombrar un valor no debe reinterpretar en
  /// silencio una preferencia ya guardada.
  String get storageValue => switch (this) {
        SyncDirection.download => 'download',
        SyncDirection.upload => 'upload',
        SyncDirection.both => 'both',
      };

  /// Cualquier valor desconocido (persistencia de una versión más nueva,
  /// dato corrupto) cae a [download] -- el modo que nunca escribe hacia el
  /// servidor, el más seguro ante la duda.
  static SyncDirection fromStorage(String? value) => switch (value) {
        'download' => SyncDirection.download,
        'upload' => SyncDirection.upload,
        'both' => SyncDirection.both,
        _ => SyncDirection.download,
      };

  /// Etiqueta para el selector de `SyncSettingsPage`.
  String get label => switch (this) {
        SyncDirection.download => 'Descargar',
        SyncDirection.upload => 'Subir',
        SyncDirection.both => 'Ambos',
      };
}

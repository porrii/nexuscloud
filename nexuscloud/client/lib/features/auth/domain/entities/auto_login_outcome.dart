/// Resultado de intentar reanudar una sesión guardada al arrancar la app.
///
/// La transición de pantalla en sí siempre la dirige `userStream`
/// (login/logout/caducidad de sesión usan el mismo mecanismo desde
/// cualquier punto de la app) -- este enum solo decide si conviene
/// mostrar un aviso adicional en la pantalla de login.
enum AutoLoginOutcome {
  /// No había token/URL de servidor guardados (primer arranque, o tras un
  /// logout). No hace falta ningún aviso.
  noSavedSession,

  /// El token guardado seguía siendo válido -- sesión restaurada en
  /// silencio.
  restored,

  /// El servidor confirmó (401) que la sesión ya no es válida. El token
  /// se borra; no hace falta un aviso especial, es un login normal.
  sessionExpired,

  /// No se pudo contactar con el servidor -- NO se sabe si la sesión
  /// sigue siendo válida (§101), así que el token se conserva y se avisa
  /// explícitamente en vez de fallar en silencio en cada arranque.
  networkError,
}

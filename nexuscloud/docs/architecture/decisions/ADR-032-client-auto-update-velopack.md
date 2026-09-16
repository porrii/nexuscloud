# ADR-032: Auto-actualización del cliente de escritorio con Velopack

## Estado

Aceptado.

## Contexto

El cliente de escritorio de Windows se empaquetaba como MSIX sin firmar
(`sign_msix: false`, ver el comentario histórico que llevaba
`pubspec.yaml`): una decisión ya tomada y funcional para *instalar* sin
certificado, apoyada en "Modo de desarrollador" o `Add-AppxPackage
-AllowUnsigned`. Pero instalar no es actualizar: el mecanismo nativo de
auto-actualización de MSIX (`AppInstaller`) no funciona en absoluto sin un
certificado de firma real -- ni siquiera con uno autofirmado instalado a
mano en `TrustedPeople`. Se confirmó investigando explícitamente a
petición del usuario, que además descartó comprar un certificado de firma
real: no es un rodeo posible, es un callejón sin salida para el objetivo
real (actualizaciones automáticas).

Se investigaron tres vías gratuitas:

1. **SignPath Foundation** (signpath.io) -- firma real y gratis vía un
   pipeline gestionado, pero exige que el proyecto sea open source con un
   repositorio público. Este repositorio es privado.
2. **Certificado autofirmado** -- gratis, pero el propio `AppInstaller` de
   MSIX no soporta auto-actualización con un certificado autofirmado,
   confirmado arriba. Descartado.
3. **Velopack** (velopack.io, sucesor de Squirrel.Windows) -- gratis, sin
   certificado de ningún tipo, con el único coste real de un aviso de
   SmartScreen la primera vez que se ejecuta un binario nuevo (blando, no
   un bloqueo). Elegida.

## Decisión

1. **Velopack sustituye a MSIX por completo**, no coexiste con él:
   mantener dos mecanismos de instalación de Windows en paralelo sin que
   ninguno aporte algo que el otro no tenga ya no compensa el coste de
   mantenimiento. Se retiró `msix`/`msix_config` de `pubspec.yaml`.
   `nexus_startup_task` (StartupTask de MSIX) queda sin ejercitarse por lo
   mismo -- sin cambio de código, porque `LaunchAtStartupService` ya tenía
   una rama de registro clásico para el caso "no empaquetado en MSIX", que
   es exactamente la que ahora aplica bajo Velopack (verificado leyendo su
   lógica de detección: pregunta a la StartupTask, y si no hay ninguna
   declarada -- que es el caso bajo Velopack -- usa el registro).
2. **El "check" de actualizaciones es HTTP+JSON reimplementado en Dart,
   sin SDK ni FFI.** No existe un SDK oficial de Dart/Flutter maduro para
   Velopack (`velopack_flutter` en pub.dev es v0.1.0 y su propio autor
   dice que no está lista para producción). Pero el feed de Velopack
   (`releases.<canal>.json`) es JSON plano, documentado y estable, así que
   `lib/features/update/data/update_check_service.dart` simplemente hace
   un `GET` y compara versiones -- sin ningún riesgo real por no tener SDK.
3. **El "apply" SÍ usa el `Update.exe` que Velopack coloca junto a la
   instalación, invocado como subproceso** -- confirmado ejecutando
   `Update.exe apply --help` contra un paquete real compilado durante el
   desarrollo (no supuesto de memoria): `Update.exe apply --package
   <nupkg> --waitPid <pid> --silent` es un comando de CLI público y
   documentado. Sin `--norestart`, reinicia la app automáticamente tras
   aplicar -- por eso `UpdateApplyService.applyAndRestart` lanza
   `Update.exe` como proceso DESTACADO y termina el proceso propio con
   `exit(0)` justo después: `--waitPid` hace que `Update.exe` espere a que
   ESTE proceso muera antes de sustituir sus propios ficheros abiertos: si
   en vez de eso se esperara aquí a que `Update.exe` termine, ninguno de
   los dos avanzaría nunca.
4. **El servidor NexusCloud hace de proxy hacia GitHub Releases -- el
   cliente nunca habla con GitHub directamente ni lleva ningún token
   propio.** Un repositorio privado exige un token para descargar sus
   releases; ese token no puede viajar dentro de un cliente instalado
   (sería un secreto compartido por todos los usuarios, extraíble del
   binario, no rotable por persona). `internal/clientupdates.Proxy`
   guarda el token solo en el entorno del servidor
   (`NEXUSCLOUD_CLIENT_UPDATES_GITHUB_TOKEN`, nunca en `config.yaml`,
   mismo criterio exacto que `NEXUSCLOUD_BACKUP_REMOTE_TOKEN`, ADR-029) y
   expone `/api/v1/public/client-updates/*` sin sesión (instalar/
   actualizar debe funcionar antes de poder haber iniciado sesión, mismo
   criterio que los enlaces públicos de compartición) pero solo si
   `clientUpdates.enabled=true` -- secure by default, igual que
   `web.enabled`/`sharing.publicLinksEnabled`.
5. **El proxy nunca reenvía una URL o nombre arbitrario**: resuelve él
   mismo la última release de GitHub que tenga un feed de Velopack
   (examinando como mucho las últimas 10, no confiando a ciegas en "la más
   reciente" -- por si este repositorio publica alguna vez otro tipo de
   release entre dos releases del cliente) y solo permite descargar un
   asset cuyo NOMBRE coincida exacto con uno real de esa release.
6. **Alcance inicial: solo paquete completo ("Full"), sin actualizaciones
   delta.** Las delta de Velopack ahorran ancho de banda pero exigen
   mantener un caché local de paquetes anteriores y una lógica de
   fallback-a-completo; para el volumen de uso de este proyecto (personal,
   self-hosted) no compensa la complejidad ahora.
7. **Nunca se descarga ni se aplica nada sin que el usuario lo pida
   explícitamente en cada paso** (comprobación automática silenciosa al
   abrir la app, pero descargar y aplicar son siempre botones que hay que
   pulsar): la app se auto-modifica, eso merece confirmación, no
   automatismo silencioso.

## Consecuencias

- Empaquetar una versión de Windows es ahora
  `deploy/scripts/package-client-windows.ps1` (`flutter build windows` +
  `vpk pack`), no `dart run msix:create`. Requiere la herramienta `vpk`
  (`dotnet tool install -g vpk`) además de Flutter.
- Publicar sigue siendo manual (`vpk upload github --token ... --publish`
  contra el repositorio real) -- no existe hoy ningún job de release en
  `.github/workflows/ci.yml`, y añadir uno no se ha pedido.
- **Verificado end-to-end de verdad, no solo en teoría**, durante el
  desarrollo: se compiló y empaquetó el cliente real dos veces (v1.0.0 y
  v1.0.1), se publicaron ambas como releases reales (de prueba, borradas
  después) en el propio repositorio privado, se instaló v1.0.0 de verdad
  en una carpeta temporal, y se ejecutó `Update.exe apply` real contra esa
  instalación real -- confirmando `Package version 1.0.1 applied
  successfully` y el manifiesto interno (`current\sq.version`) reflejando
  `1.0.1` de verdad. Limpieza completa después (desinstalado, releases de
  prueba borradas).
- **Límite honesto de esta verificación**: no se hizo una llamada real
  contra la API de GitHub encarnando el token dentro del propio proceso
  del servidor Go (`internal/clientupdates.Proxy`) durante esta sesión --
  el entorno de ejecución bloqueó explícitamente extraer el token de `gh`
  a una variable/fichero para pasárselo a un proceso que no es `gh`
  (control de manejo de credenciales, no una limitación técnica del
  código). Ese camino queda cubierto por 7 tests unitarios reales contra
  un servidor HTTP falso (cabecera `Authorization`, caché, asset
  desconocido, ninguna release con feed, etc.), pero no por una llamada
  real a `api.github.com`. Si se quiere cerrar también esa verificación,
  hay que arrancar el servidor con un token real en
  `NEXUSCLOUD_CLIENT_UPDATES_GITHUB_TOKEN` (puesto por el propio usuario,
  p.ej. con `gh auth token`) y comprobar `GET
  /api/v1/public/client-updates/releases.json` a mano.
- La descarga del paquete SÍ verifica su SHA-256 (que el propio feed de
  Velopack publica) antes de pasárselo a `Update.exe apply` -- un
  paquete a medias o corrupto nunca llega a aplicarse.

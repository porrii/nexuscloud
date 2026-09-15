# Primeros pasos (Linux, sin entorno gráfico)

Guía de arranque, de cero a tener NexusCloud funcionando en un servidor
Linux headless — todo por línea de comandos, nada asume una sesión de
escritorio ni un navegador local.

## 1. Instalar

Desde la raíz del repositorio clonado:

```sh
sudo ./install.sh
```

Esto, en un solo paso: instala Go si hace falta (descarga oficial de
go.dev, nunca el paquete de la distro), compila el binario, crea el
usuario de servicio `nexuscloud`, genera `/etc/nexuscloud/config.yaml` con
valores seguros por defecto, aplica las migraciones de base de datos, e
instala (pero **no arranca**) el servicio de systemd.

¿Quieres también la interfaz web (útil si vas a administrar NexusCloud
desde el navegador de otro equipo en tu red, no solo por comandos)?

```sh
sudo ./install.sh --web
```

Instala Node.js LTS si hace falta y compila la web real. Sin `--web`
(el caso por defecto), el binario ni siquiera la incluye — puedes añadirla
después sin reinstalar desde cero, ver
[`mantenimiento.md`](mantenimiento.md#añadir-o-quitar-la-interfaz-web).

## 2. Revisar la configuración

```sh
sudo -e /etc/nexuscloud/config.yaml
```

Lo que más vale la pena mirar en un primer arranque:
- `storage.dataDir`: dónde vive todo (base de datos, archivos, backups por
  defecto). Si tienes un disco grande aparte, puedes moverlo ahí antes
  incluso del primer arranque.
- `server.port`: 8080 por defecto.
- `trash.retentionDays` / `versioning.maxVersionsPerFile`: cuánto tiempo
  se conservan los archivos borrados / versiones antiguas.

No hace falta tocar nada para empezar — los valores por defecto son
seguros (sin web pública, sin registro público, rate limiting activo).

## 3. Crear el primer usuario

```sh
sudo -u nexuscloud nexuscloud --config /etc/nexuscloud/config.yaml admin create-user
```

Sin `--username`/`--password`, los pide de forma interactiva (la
contraseña sin eco en pantalla). El primer usuario creado en la instancia
recibe automáticamente el rol `super_admin`. Nunca se permite `admin` como
contraseña, ni contraseñas de menos de 8 caracteres.

## 4. Comprobar que todo está en orden

```sh
sudo -u nexuscloud nexuscloud --config /etc/nexuscloud/config.yaml doctor
```

Real, contra una instalación recién hecha:

```
PASS      Configuration        configVersion=1
PASS      Database             sqlite, esquema v7
PASS      Storage              /var/lib/nexuscloud/storage
PASS      Port                 0.0.0.0:8080
WARNING   HTTPS                TLS no configurado; aceptable en LAN, revisar antes de exponer a Internet (§67)
PASS      Web pública          desactivada (secure by default, §3/§47)
PASS      Public registration  desactivado
```

El `WARNING` de HTTPS es normal si NexusCloud solo va a vivir dentro de tu
red local (LAN) — solo hace falta resolverlo si vas a exponerlo a
Internet. Cualquier `FAIL` hay que arreglarlo antes de seguir.

## 5. Arrancar

```sh
sudo systemctl start nexuscloud
systemctl status nexuscloud
journalctl -u nexuscloud -f      # logs en vivo
```

## 6. Guardar y organizar tus primeros archivos

Ya sin necesitar la interfaz web para nada — todo esto es lo mismo que
harías arrastrando ficheros en un explorador, pero por comandos:

```sh
nexuscloud --config /etc/nexuscloud/config.yaml files mkdir --username tu-usuario /Documentos
nexuscloud --config /etc/nexuscloud/config.yaml files upload --username tu-usuario ./informe.pdf /Documentos/informe.pdf
nexuscloud --config /etc/nexuscloud/config.yaml files list --username tu-usuario /Documentos
```

Ver [`comandos.md`](comandos.md) para el resto de operaciones de archivo
(descargar, mover, borrar) y [`administracion.md`](administracion.md) para
añadir más usuarios, grupos y doble factor.

## 7. Si vas a usar el cliente de escritorio

El cliente Flutter (Windows/Linux) sincroniza una carpeta local con tu
servidor automáticamente, sin tener que subir/bajar archivos a mano cada
vez. Ver [`cliente-de-escritorio.md`](cliente-de-escritorio.md).

## Si algo no arranca

Ver [`mantenimiento.md`](mantenimiento.md) y
[`preguntas-frecuentes.md`](preguntas-frecuentes.md) — casi todo lo
recurrente (puerto ocupado, permisos, `doctor` en rojo) tiene una
respuesta directa ahí.

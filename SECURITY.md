# Política de seguridad

## Versiones soportadas

Solo la última release recibe correcciones de seguridad.

## Reportar una vulnerabilidad

Si encuentras un problema de seguridad, **no abras un issue público**.
Repórtalo en privado desde la pestaña
[Security → Report a vulnerability](https://github.com/porrii/nexuscloud/security/advisories/new)
de este repositorio, con el mayor detalle posible (pasos para
reproducirlo, versión afectada, impacto).

Se responde en un plazo razonable y, si se confirma, se publica una
release con la corrección antes de hacer público ningún detalle.

## Alcance

NexusCloud está pensado para desplegarse en una red de confianza (LAN) o
detrás de tu propio dominio con TLS -- ver
[docs/security.md](nexuscloud/docs/security.md) para el modelo de
seguridad completo (autenticación, control de acceso, cifrado en
reposo/tránsito, superficie pública por defecto).

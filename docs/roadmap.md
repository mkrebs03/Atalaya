# Roadmap

Atalaya prioriza una instalación pequeña, privada y operable sobre una red Tailscale.
La primera release pública concentra el producto en un único administrador y un
servidor Linux con agentes Linux adicionales.

## Primera release

- Dashboard autenticado para hosts Linux.
- Métricas actuales e históricas de CPU, memoria, uptime y filesystems.
- Checks HTTP/HTTPS y TCP desde el servidor central.
- Alertas sostenidas para hosts, checks, CPU, memoria y filesystems.
- Incidentes con notificaciones Discord y entregas auditables.
- Enrolamiento de agentes mediante tokens de un solo uso.
- Instalación y operación con un ejecutable, SQLite, `systemd` y Tailscale Serve.
- Backup y restauración con clave maestra separada.
- Artefactos Linux `amd64` y `arm64` con checksums SHA-256.

## Limitaciones conocidas

- Existe un único administrador.
- El acceso de la aplicación es privado y requiere Tailscale.
- El servidor central es un único proceso y SQLite es la única base de datos.
- Discord es el único proveedor de notificaciones incluido.
- Si el servidor central cae por completo, la instalación no puede enviar por sí misma
  una alerta de esa caída.

## Después de la primera release

Las siguientes mejoras quedan deliberadamente fuera del primer alcance:

- Watchdog independiente en otro host.
- Más proveedores de notificaciones y checks adicionales.
- Silencios y ventanas de mantenimiento.
- Multiusuario, roles y equipos.
- Alta disponibilidad, replicación y paquetes nativos de distribución.

Las nuevas capacidades deben conservar los límites de seguridad y privacidad descritos
en la [especificación](../spec.md) y en la [documentación de operación](security-and-operations.md).

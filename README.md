# Atalaya

[![CI](https://github.com/mkrebs03/Atalaya/actions/workflows/ci.yml/badge.svg)](https://github.com/mkrebs03/Atalaya/actions/workflows/ci.yml)

Atalaya es una plataforma de monitorización privada para hosts Linux y servicios
self-hosted dentro de una red Tailscale. Reúne métricas actuales, histórico,
checks de servicio y alertas en una interfaz web sencilla, con un único binario,
SQLite y `systemd`.

El acceso de la aplicación permanece dentro de la tailnet: Tailscale Serve termina
HTTPS y el servidor solo escucha en loopback. Atalaya no necesita Docker, Node.js,
Go, nginx ni una base de datos externa en runtime.

## Instalación rápida

Necesitas Linux con `systemd`, Tailscale instalado y conectado, y `curl` o `wget`.
El instalador detecta la arquitectura, valida SHA-256 y configura los servicios; no
instala Tailscale automáticamente.

### Servidor

En el host que alojará Atalaya, ejecuta la última release estable desde una terminal:

```bash
curl -fsSL https://github.com/mkrebs03/Atalaya/releases/latest/download/install.sh \
  | sudo sh -s -- server
```

El rol `server` instala también el agente local. Crea la base de datos, la clave
maestra y el administrador inicial; durante el proceso se te solicitará establecer
la contraseña de forma segura. Al terminar, abre la URL HTTPS del host dentro de la
tailnet, normalmente `https://<servidor>.ts.net`.

### Agente Linux remoto

En cada host adicional, ejecuta el instalador indicando la URL HTTPS del servidor:

```bash
curl -fsSL https://github.com/mkrebs03/Atalaya/releases/latest/download/install.sh \
  | sudo sh -s -- agent --server-url https://<servidor>.ts.net
```

Después, crea un token temporal en **Gestionar** en el servidor y enrola el agente:

```bash
sudo /usr/local/bin/atalaya agent enroll \
  --config /etc/atalaya/agent.yaml \
  --display-name "<nombre-del-host>"
```

El token se solicita sin eco. No lo incluyas en argumentos, variables persistentes ni
en el historial del shell. La CLI queda instalada en `/usr/local/bin/atalaya` y los
servicios se ejecutan como el usuario restringido `atalaya`.

Para usar una versión fijada, el canal `dev`, actualizar, verificar, consultar logs o
hacer backup/restore, sigue la [guía de seguridad y operación](docs/security-and-operations.md).

## Qué incluye

- Métricas de CPU, memoria, uptime, Tailscale y filesystems de hosts Linux.
- Histórico de métricas con muestras crudas, agregados horarios y retención configurable.
- Checks HTTP/HTTPS y TCP ejecutados desde el servidor central.
- Reglas de alerta para hosts, checks, CPU, memoria y filesystems.
- Incidentes con estados `pending`, `firing` y `resolved`, sin duplicar notificaciones.
- Notificaciones Discord mediante webhooks cifrados y entregas auditables.
- Enrolamiento de agentes con tokens de un solo uso y credenciales restringidas.
- Instalación, backup, restore y actualización mediante CLI e instalador `systemd`.
- Builds reproducibles para Linux `amd64` y `arm64` con `CGO_ENABLED=0`.

## Alcance de la primera release

La primera release está enfocada en un único administrador y despliegues privados
con Tailscale. Los hosts monitorizados deben ejecutar Linux; el servidor central
también ejecuta el scheduler, los checks y el evaluador de alertas.

Quedan fuera de este alcance multiusuario, alta disponibilidad, replicación de
SQLite, acceso público a Internet, OAuth/SSO, agentes Windows o macOS, buffering
local durante caídas del servidor y un watchdog externo. El primer proveedor de
notificaciones es Discord.

## Arquitectura

```text
Navegador dentro de la tailnet
             |
             | HTTPS mediante Tailscale Serve
             v
      Servidor Atalaya + SQLite
       |      |       |       |
       |      |       |       +-- Notificaciones Discord
       |      |       +---------- Checks HTTP/TCP
       |      +------------------ Alertas, histórico y retención
       +------------------------- Pull autenticado de agentes
                                  |
                           Agentes Linux remotos
```

El servidor y cada agente escuchan únicamente en loopback. Tailscale Serve publica
HTTPS dentro de la tailnet en los puertos 443 y 9474, según el rol instalado.

## Operación y mantenimiento

La [guía de seguridad y operación](docs/security-and-operations.md) contiene los
comandos para comprobar servicios y health checks, consultar logs, actualizar releases,
usar el canal `dev`, crear backups y restaurar una instalación offline.

No expongas directamente los puertos loopback ni desactives la validación TLS de los
agentes. Las ACLs de Tailscale son la frontera de acceso de la instalación.

## Desarrollo

El entorno de desarrollo, las versiones fijadas y los comandos `make lint`,
`make test`, `make build` y `make test-e2e` están documentados en
[`CONTRIBUTING.md`](CONTRIBUTING.md).

## Documentación

- [Especificación del producto](spec.md): alcance, comportamiento y limitaciones.
- [Arquitectura](docs/architecture.md): componentes, flujos y límites.
- [API y protocolos](docs/api.md): contratos entre UI, servidor y agentes.
- [Modelo de dominio](docs/domain-model.md): entidades, estados y persistencia.
- [Seguridad y operación](docs/security-and-operations.md): despliegue, red, secretos,
  backup, restore y actualización.
- [Dependencias](docs/dependencies.md): dependencias directas y licencias.
- [Roadmap](docs/roadmap.md): alcance de release y trabajo posterior.
- [Contribuir](CONTRIBUTING.md): entorno, estándares y verificación.
- [Seguridad](SECURITY.md): reporte privado de vulnerabilidades.
- [Licencia](LICENSE): Apache License 2.0.

## Licencia

Atalaya se distribuye bajo [Apache License 2.0](LICENSE). El código de terceros
conserva sus propias licencias; consulta el [registro de dependencias](docs/dependencies.md)
para las dependencias directas.

## Seguridad

Atalaya está diseñado para redes privadas y no debe exponerse directamente a Internet.
No incluyas contraseñas, tokens, webhooks ni claves maestras en issues, logs o commits.
Si encuentras una vulnerabilidad, evita publicar detalles explotables hasta coordinar
una corrección con los mantenedores.

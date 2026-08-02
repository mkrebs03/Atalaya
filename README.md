# Atalaya

[![CI](https://github.com/mkrebs03/Atalaya/actions/workflows/ci.yml/badge.svg)](https://github.com/mkrebs03/Atalaya/actions/workflows/ci.yml)

Atalaya es una plataforma de monitorización privada para hosts Linux y servicios
self-hosted dentro de una red Tailscale. Reúne métricas actuales, histórico,
checks de servicio y alertas en una interfaz web sencilla, con un único binario,
SQLite y `systemd`.

El acceso de la aplicación permanece dentro de la tailnet: Tailscale Serve termina
HTTPS y el servidor solo escucha en loopback. Atalaya no necesita Docker, Node.js,
Go, nginx ni una base de datos externa en runtime.

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

## Instalación desde una release

Requisitos: Linux con `systemd`, Tailscale instalado y conectado, y `curl` o
`wget`. El instalador detecta la arquitectura, valida SHA-256 y configura los
servicios; no instala Tailscale automáticamente.

### Servidor

Descarga una versión concreta, inspecciona el instalador y ejecútalo:

```bash
ATALAYA_VERSION=v0.0.1
curl --fail --location --proto '=https' --proto-redir '=https' \
  "https://github.com/mkrebs03/Atalaya/releases/download/${ATALAYA_VERSION}/install.sh" \
  --output install.sh

sh -n install.sh
sudo sh install.sh server --version "$ATALAYA_VERSION"
```

El rol `server` instala también el agente local. Durante la instalación se crean la
base de datos, la clave maestra y el administrador inicial; la contraseña se solicita
interactivamente y no aparece en la línea de comandos.

### Agente remoto

En el host remoto descarga el mismo `install.sh` y ejecuta:

```bash
sudo sh install.sh agent \
  --version "$ATALAYA_VERSION" \
  --server-url https://<servidor> \
  --display-name "<nombre-del-host>"
```

Después, crea un token temporal en la vista **Gestionar** del servidor y enrola el
agente. El token se solicita sin eco; no lo incluyas en argumentos ni en el historial
del shell:

```bash
sudo /usr/local/bin/atalaya agent enroll \
  --config /etc/atalaya/agent.yaml \
  --display-name "<nombre-del-host>"
```

La CLI queda instalada en `/usr/local/bin/atalaya`. Los servicios usan esa ruta
absoluta y se ejecutan como el usuario restringido `atalaya`.

### Canal rolling de desarrollo

El canal `dev` se actualiza automáticamente después de cada CI exitoso sobre `main`.
Es útil para probar el estado integrado más reciente, pero es inestable y no debe
usarse con datos de producción. Sus artefactos y el tag `dev` pueden cambiar.

Descarga el instalador del canal y ejecuta:

```bash
curl --fail --location --proto '=https' --proto-redir '=https' \
  "https://github.com/mkrebs03/Atalaya/releases/download/dev/install.sh" \
  --output install.sh

sh -n install.sh
sudo sh install.sh server --channel dev
```

Para actualizar una instalación existente:

```bash
sudo sh install.sh upgrade --channel dev
```

`--channel dev` no se combina con `--version`. Las releases oficiales siguen usando
tags estables inmutables y `--version` explícita.

## Operación básica

```bash
sudo systemctl is-active atalaya-server.service
sudo systemctl is-active atalaya-agent.service
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:9474/agent/v1/healthz
sudo /usr/local/bin/atalaya verify --config /etc/atalaya/server.yaml
```

Los logs se consultan con `journalctl`:

```bash
sudo journalctl -u atalaya-server.service -u atalaya-agent.service --no-pager
```

No expongas directamente los puertos loopback ni desactives la validación TLS de
los agentes. Las ACLs de Tailscale son la frontera de acceso de la instalación.

## Backup y restore

El backup de datos no contiene `master.key`. Exporta la clave maestra por separado y
custodia ambos archivos mediante mecanismos distintos:

```bash
sudo install -d -m 0700 /var/backups/atalaya
sudo /usr/local/bin/atalaya backup \
  --config /etc/atalaya/server.yaml \
  --output /var/backups/atalaya/data.tar.gz
sudo /usr/local/bin/atalaya backup-key \
  --config /etc/atalaya/server.yaml \
  --output /var/backups/atalaya/master.key \
  --yes
sudo /usr/local/bin/atalaya verify --config /etc/atalaya/server.yaml
```

La restauración es offline: detener servicios, recuperar datos y clave con permisos
correctos, ejecutar `migrate`, ejecutar `verify` y arrancar solo después de ambas
verificaciones. El procedimiento completo y las garantías de seguridad están en
[`docs/security-and-operations.md`](docs/security-and-operations.md).

## Desarrollo

Requisitos de desarrollo:

- Go `1.26.5`.
- Node.js `24.18.0`.
- npm `11.16.0`.
- GNU Make.
- Chromium de Playwright para las pruebas E2E.

Las versiones están fijadas en [`.tool-versions`](.tool-versions) y en el lockfile
de frontend. Con las dependencias instaladas:

```bash
make lint
make test
make build
make install-e2e-browser
make test-e2e
```

`make build` genera `build/atalaya-linux-amd64` y
`build/atalaya-linux-arm64`. Para desarrollo interactivo, `make dev-server` y
`make web-dev` levantan el backend y Vite en terminales separadas.

`make package-release VERSION=0.0.1` genera los dos binarios, `install.sh` y
`SHA256SUMS` en `build/release/`, igual que el empaquetado de GitHub Actions.

## Documentación

- [Especificación del producto](spec.md): alcance, comportamiento y limitaciones.
- [Arquitectura](docs/architecture.md): componentes, flujos y límites.
- [API y protocolos](docs/api.md): contratos entre UI, servidor y agentes.
- [Modelo de dominio](docs/domain-model.md): entidades, estados y persistencia.
- [Seguridad y operación](docs/security-and-operations.md): despliegue, red, secretos,
  backup y actualización.
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

# Atalaya

Atalaya es un dashboard personal para observar hosts Linux y servicios self-hosted
dentro de una red Tailscale. La Fase 1A agrega al bootstrap reproducible configuracion
YAML estricta, clave maestra, SQLite con migraciones y creacion local del administrador.
La interfaz Vue embebida y la salud de proceso siguen disponibles mientras el resto de
la autenticacion de Fase 1 se implementa.

El producto completo sigue el contrato de [`spec.md`](spec.md). El indice y la
precedencia de decisiones tecnicas estan en [`docs/README.md`](docs/README.md).

## Requisitos de desarrollo

- Go `1.26.5`.
- Node.js `24.18.0`.
- npm `11.16.0`.
- GNU Make.
- Chromium de Playwright, solo para `make test-e2e`.

Las versiones de Go y Node estan fijadas en [`.tool-versions`](.tool-versions), y npm
tambien queda fijado en [`web/package.json`](web/package.json). Con un gestor compatible
con asdf basta ejecutar `asdf install`; en otros entornos instala esas versiones
exactas antes de usar los comandos del proyecto.

## Verificacion y build

```bash
make lint
make test
make build
```

`make build` genera:

- `build/atalaya-linux-amd64`
- `build/atalaya-linux-arm64`

Ambos artefactos se compilan con `CGO_ENABLED=0`; el frontend ya esta incluido y
Node.js no es necesario en runtime. El build tambien compila la prueba de compatibilidad
de SQLite, Argon2id y YAML para Linux `amd64` y `arm64`, sin ejecutar el binario de
prueba cruzado ni dejarlo como artefacto. Para inyectar una version sin agregar
timestamps:

```bash
make build VERSION=0.1.0
```

## Arranque local

Primero prepara una configuracion, una clave raw de exactamente 32 bytes y el directorio
de datos. Los comandos no generan ni reemplazan la clave maestra:

```bash
mkdir -p build/dev
install -m 0600 /dev/null build/dev/master.key
dd if=/dev/urandom of=build/dev/master.key bs=32 count=1 conv=notrunc status=none

cat > build/dev/server.yaml <<'YAML'
listen: 127.0.0.1:8080
mode: development
public_url: http://127.0.0.1:8080
database_path: atalaya.db
master_key_path: master.key
YAML
chmod 0600 build/dev/server.yaml
```

Los paths relativos se resuelven desde el directorio del YAML. Para construir, migrar,
crear el administrador y arrancar el ejecutable unico:

```bash
make build
ARCH=amd64 # usa arm64 en Linux aarch64
./build/atalaya-linux-${ARCH} migrate --config build/dev/server.yaml
./build/atalaya-linux-${ARCH} admin create --config build/dev/server.yaml
./build/atalaya-linux-${ARCH} server --config build/dev/server.yaml
```

`admin create` exige una terminal interactiva y solicita la password dos veces sin
eco. El servidor puede arrancar antes de crear el administrador, pero nunca lo crea
automaticamente. Luego comprueba la salud binaria:

```bash
curl -i http://127.0.0.1:8080/healthz
```

Para trabajar con hot reload, usa una copia del YAML con
`public_url: http://127.0.0.1:5173`, ejecuta `make dev-server CONFIG=<esa-copia>` y
`make web-dev` en terminales separadas. Vite queda en `http://127.0.0.1:5173` y envia
`/api` y `/healthz` al servidor Go en `127.0.0.1:8080`.

El listener HTTP de Go siempre debe ser loopback (`localhost`, `127.0.0.1` o `::1`) en
todos los modos; Tailscale Serve publica HTTPS sin abrir el proceso a otra interfaz.
Produccion exige un origen `https://`. El modo `development` solo acepta un origen
HTTP si tambien es loopback. `public_url` representa un origen estricto: no admite
usuario, path, delimitadores de query o fragmento aunque esten vacios, ni puerto
explicito vacio. Los errores de parseo no reproducen el valor recibido.

## Pruebas end-to-end

Instala primero las dependencias fijadas y Chromium, y luego ejecuta el flujo:

```bash
make install-e2e-browser
make test-e2e
```

Los targets usan un stamp dependiente de `package.json`, el lockfile y
`.tool-versions`, por lo que `npm ci` se repite solo cuando cambia una de esas entradas
o falta `node_modules`. Playwright siempre arranca el binario Go host recien construido
desde `build/atalaya`; no reutiliza otro servidor ni usa Vite Preview. Cada ejecucion
crea config, clave `0600` y DB aisladas bajo `build/e2e`.

## CLI de Fase 1A

`atalaya version` y las ayudas terminan con codigo `0`. Errores operativos y los
comandos reservados `agent`, `backup` y `verify` terminan con `1`. Tambien usan `1` una
clave o DB invalida, un fallo de terminal, passwords no coincidentes o un administrador
activo ya existente. Uso, argumentos, YAML o valores de configuracion invalidos,
incluidos comando ausente o desconocido, terminan con `2`.

Los comandos implementados de este slice comparten el mismo archivo:

```text
atalaya server [--config <path>]
atalaya migrate [--config <path>]
atalaya admin create [--config <path>] [--username <name>]
```

El default es `/etc/atalaya/server.yaml`. No existen overrides de configuracion por
variables de entorno en Fase 1A.

Login, sesiones, CSRF, reset de password, agentes y operacion Tailscale se implementan
en los siguientes slices. Consulta el
[plan de implementacion](docs/implementation-plan.md) para el alcance y las condiciones
de salida.

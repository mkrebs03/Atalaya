# Atalaya

Atalaya es un dashboard personal para observar hosts Linux y servicios self-hosted
dentro de una red Tailscale. La Fase 0 ya entrega un bootstrap reproducible: una CLI
Go, una interfaz Vue embebida, salud de proceso, pruebas y builds Linux cruzados.

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

Para probar el ejecutable unico:

```bash
make build
ARCH=amd64 # usa arm64 en Linux aarch64
./build/atalaya-linux-${ARCH} server \
  --mode development \
  --listen 127.0.0.1:8080 \
  --public-url http://127.0.0.1:8080
```

Luego abre `http://127.0.0.1:8080` y comprueba la salud binaria:

```bash
curl -i http://127.0.0.1:8080/healthz
```

Para trabajar con hot reload, ejecuta `make dev-server` y `make web-dev` en terminales
separadas. Vite queda en `http://127.0.0.1:5173` y envia `/api` y `/healthz` al servidor
Go en `127.0.0.1:8080`.

El listener HTTP de Go siempre debe ser loopback (`localhost`, `127.0.0.1` o `::1`) en
todos los modos; Tailscale Serve publica HTTPS sin abrir el proceso a otra interfaz.
Produccion exige un origen `https://`. El modo `development` solo acepta un origen
HTTP si tambien es loopback. `--public-url` representa un origen estricto: no admite
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
desde `build/atalaya`; no reutiliza otro servidor ni usa Vite Preview.

## CLI de Fase 0

`atalaya version` y las ayudas terminan con codigo `0`. Errores operativos y los
comandos reservados `agent`, `backup` y `verify` terminan con `1`. Uso o configuracion
invalidos, incluidos comando ausente o desconocido, terminan con `2`.

La persistencia, autenticacion, agentes y operacion Tailscale se implementan en las
fases siguientes. Consulta el [plan de implementacion](docs/implementation-plan.md)
para el alcance y las condiciones de salida.

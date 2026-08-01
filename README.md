# Atalaya

Atalaya es un dashboard personal para observar hosts Linux y servicios self-hosted
dentro de una red Tailscale. Tras completar persistencia, autenticacion y la Fase 2,
la Fase 3 agrega series historicas, rollups horarios, retencion configurable y
graficos con huecos explicitos. El alcance exacto de cada slice permanece definido por
la documentacion.

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

Los paths relativos se resuelven desde el directorio del YAML. Para construir y
preparar servidor y administrador:

```bash
make build
ARCH=amd64 # usa arm64 en Linux aarch64
./build/atalaya-linux-${ARCH} migrate --config build/dev/server.yaml
./build/atalaya-linux-${ARCH} admin create --config build/dev/server.yaml
```

`admin create` exige una terminal interactiva y solicita la password dos veces sin
eco. El servidor puede arrancar antes de crear el administrador, pero nunca lo crea
automaticamente.

Para el agente local, prepara los directorios padre y ejecuta bootstrap despues de
migrar/crear admin. Bootstrap genera `agent.yaml` modo exacto `0640` y credential modo
exacto `0600`; no instala unidades systemd ni configura Tailscale:

```bash
./build/atalaya-linux-${ARCH} bootstrap local-agent \
  --server-config build/dev/server.yaml \
  --agent-config build/dev/agent.yaml \
  --agent-url https://oracle.example.ts.net:9474
```

El YAML generado contiene exactamente estas claves; paths relativos se resuelven
desde su directorio:

```yaml
listen: 127.0.0.1:9474
server_url: http://127.0.0.1:8080
advertised_url: https://oracle.example.ts.net:9474
credential_path: agent-credential
```

`server_url` se deriva del `public_url` del server config y `advertised_url` reproduce
el `--agent-url` validado. Configura manualmente Tailscale Serve para publicar el
listener 9474, inicia primero agente y despues inicia o reinicia servidor, en terminales
separadas:

```bash
./build/atalaya-linux-${ARCH} agent --config build/dev/agent.yaml
./build/atalaya-linux-${ARCH} server --config build/dev/server.yaml
```

Finalmente valida salud del servidor, salud del agente y que Oracle aparezca en la UI
autenticada:

```bash
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:9474/agent/v1/healthz
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

### Enrolar un agente remoto

Desde la UI autenticada, crea un token en Gestionar y guárdalo temporalmente en un
archivo modo `0600`. En el host remoto, con un `agent.yaml` válido, ejecuta:

```bash
atalaya agent enroll --config /etc/atalaya/agent.yaml --token-file /run/user/$(id -u)/atalaya-token
```

El archivo se elimina después de leerlo. Sin `--token-file`, el comando solicita el
token por prompt. El enrolamiento guarda un estado lateral 0600 para repetir de forma
segura una respuesta perdida y escribe la credencial sin reemplazar una credencial
válida existente.

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

## CLI implementada hasta Fase 2B

`atalaya version` y las ayudas terminan con codigo `0`. Errores operativos y los
comandos reservados `backup` y `verify` terminan con `1`. Tambien usan `1` una
clave o DB invalida, un fallo de terminal, passwords no coincidentes o un administrador
activo ya existente. Uso, argumentos, YAML o valores de configuracion invalidos,
incluidos comando ausente o desconocido, terminan con `2`.

Comandos implementados:

```text
atalaya server [--config <path>]
atalaya migrate [--config <path>]
atalaya admin create [--config <path>] [--username <name>]
atalaya admin reset-password [--config <path>]
atalaya agent --config <path>
atalaya agent enroll --config <path> [--display-name <name>] [--token-file <path>]
atalaya bootstrap local-agent --server-config <path> --agent-config <path> \
  --agent-url <url> [--display-name <name>] [--listen <addr>] \
  [--credential-path <path>]
```

Solo `server`, `migrate` y los subcomandos `admin` comparten server config con default
`/etc/atalaya/server.yaml`. `agent --config` exige path explicito y no tiene default.
Bootstrap exige `--server-config`, `--agent-config` y `--agent-url`; si se omiten sus
flags opcionales usa display name `Oracle`, listener `127.0.0.1:9474` y credential path
junto al agent config. No existen overrides por variables de entorno.

La UI autenticada permite gestionar tokens, cadencias, estado administrativo y
filesystems de hosts. Consulta el [plan de implementacion](docs/implementation-plan.md)
para el alcance y las condiciones de salida.

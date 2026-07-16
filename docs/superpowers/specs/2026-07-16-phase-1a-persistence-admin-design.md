# Fase 1A: persistencia segura y administrador inicial

Fecha: 2026-07-16

Estado: aprobado para implementacion

Este documento concreta el primer slice de la Fase 1. Implementa configuracion de
arranque, SQLite con migraciones, clave maestra, cifrado versionado, creacion del
administrador y su integracion con `atalaya server`. Si entra en conflicto con otro
documento, se aplica la precedencia de [`docs/README.md`](../../README.md):
`spec.md` y los contratos tecnicos existentes conservan mayor autoridad.

## 1. Alcance y resultado

Al finalizar:

- Los comandos `server`, `migrate` y `admin create` usan el mismo archivo YAML y el
  mismo flujo de apertura/migracion de la base.
- Una base vacia recibe el schema v1 de forma transaccional e idempotente.
- Una base Atalaya existente solo puede abrirse con su clave maestra. Una clave
  incorrecta falla sin modificar las filas logicas ni el archivo principal de la DB;
  `mode=ro` puede crear, usar o modificar sidecars WAL/SHM.
- El administrador inicial se crea localmente con password leida sin eco y queda
  protegido con Argon2id.
- El servidor mantiene SQLite abierta durante su vida, migra antes de escuchar y puede
  arrancar sin administrador. Nunca crea uno automaticamente.

Login, sesiones, CSRF, cambio o reset de password y proteccion de SPA/API pertenecen al
siguiente slice de Fase 1. La UI bootstrap, sus assets y `/healthz` conservan por ahora
el comportamiento publico de Fase 0.

## 2. Contrato de CLI y configuracion

### 2.1 Comandos

La interfaz pasa a ser:

```text
atalaya server [--config <path>]
atalaya migrate [--config <path>]
atalaya admin create [--config <path>] [--username <name>]
```

`--config` usa `/etc/atalaya/server.yaml` por defecto. No se mantienen los flags
anteriores `--listen`, `--mode` ni `--public-url`, y en este slice no existen
overrides mediante variables de entorno. Ninguno de los tres comandos admite
argumentos posicionales.

Ayuda y ejecucion exitosa usan codigo `0`. Argumentos, campos YAML o valores de
configuracion invalidos usan codigo `2`. Fallos operativos (lectura de archivos o TTY,
permisos o contenido de clave, DB, migracion, password no coincidente o administrador
activo ya existente) usan codigo `1`. Los errores no reproducen claves, passwords ni
una URL publica malformada.

### 2.2 YAML

El schema YAML de Fase 1A contiene exactamente estos campos, todos obligatorios:

```yaml
listen: 127.0.0.1:8080
mode: production
public_url: https://atalaya.example.ts.net
database_path: /var/lib/atalaya/atalaya.db
master_key_path: /etc/atalaya/master.key
```

La decodificacion usa `KnownFields(true)`, rechaza campos desconocidos, documentos
adicionales y valores ausentes o vacios. `database_path` y `master_key_path` relativos
se resuelven respecto del directorio que contiene el YAML, no respecto del working
directory.

La validacion existente sigue siendo normativa: `listen` debe apuntar a loopback;
`mode` solo puede ser `production` o `development`; produccion exige un origen HTTPS,
y development solo permite HTTP cuando listener y origen son loopback. `public_url`
sigue siendo un origen estricto sin userinfo, path, query, fragmento ni puerto
explicitamente vacio.

## 3. Apertura de SQLite y migraciones

### 3.1 Preflight, clasificacion y orden de arranque

Una DB es **fresh** solo si el path no existe o su tamano es exactamente cero. Cualquier
archivo SQLite no vacio que no tenga una tabla `schema_migrations` es **foreign** y se
rechaza, aunque no contenga tablas de usuario. Es **Atalaya corrupta** si la tabla
`schema_migrations` existe pero su forma o historia no son validas, sus versiones no
son contiguas desde 1, o faltan/estan malformados `instance_metadata`, el verificador de
clave o la tabla de administradores. Una version superior a la maxima embebida falla
como futura. Todo checksum guardado debe coincidir con el SHA-256 de la migracion
embebida correspondiente.

Una DB existente se inspecciona mediante una URI de modernc SQLite con `mode=ro`, sin
`immutable=1` y sin ejecutar ningun pragma que cambie `journal_mode`. El preflight no
emite DDL, DML ni pragmas persistentes de aplicacion. Esto garantiza que, con clave
incorrecta, el archivo principal de la DB queda identico byte a byte y sus filas
logicas no cambian. SQLite puede crear o usar sidecars WAL/SHM para obtener semantica
read-only correcta; esos sidecars no son una mutacion logica. Si el modo read-only no
puede establecer una vista consistente, el comando falla sin continuar.

`server` y `migrate` cargan y validan argumentos, YAML y clave, ejecutan el preflight y,
si la DB es Atalaya, autentican el verificador. Solo entonces abren en modo escritura.
Dentro del primer `BEGIN IMMEDIATE` vuelven a clasificar el schema y a verificar clave,
versiones y checksums antes de cualquier DDL de migracion, cerrando la carrera entre
preflight y escritura. Aplican las migraciones pendientes y habilitan WAL una unica vez
despues de que la clave se valido o el estado fresh fue revalidado.

`admin create` usa un orden mas estricto:

1. Carga configuracion y clave.
2. Hace preflight read-only, clasifica/verifica y rechaza un administrador activo ya
   existente.
3. Pide, confirma y valida el password, y calcula su hash.
4. Solo entonces abre en escritura. Cada migracion pendiente usa su propio
   `BEGIN IMMEDIATE`; antes de emitir DDL, esa transaccion revalida la clasificacion,
   clave, versiones y checksums. La migracion v1 hace commit atomico de schema y
   sentinel.
5. Tras terminar el runner, una transaccion `BEGIN IMMEDIATE` corta y separada vuelve a
   validar clave, schema y singleton, y recien entonces inserta el administrador.

Por ello un mismatch no escribe un administrador y, para un path nuevo, ni siquiera
crea la DB. Si las migraciones terminan pero el insert posterior falla, puede quedar de
forma intencional una DB migrada pero aun sin inicializar.

`server` completa el bootstrap antes de construir el handler o abrir el listener,
mantiene la DB abierta y la cierra despues del shutdown o un fallo. `migrate` cierra la
DB sin iniciar HTTP. Ambos pueden dejar una DB migrada sin administrador.

### 3.2 Conexion

Se usa `modernc.org/sqlite` con `CGO_ENABLED=0`. El DSN de escritura fija
`_pragma=foreign_keys(1)`, `_pragma=busy_timeout(5000)` y `_txlock=immediate` para que
los dos pragmas rijan en cada conexion y las transacciones del runner adquieran el lock
inmediato. `journal_mode` debe quedar en `WAL`, habilitado una sola vez despues del
preflight/revalidacion. Los tests verifican los pragmas efectivos con al menos dos
valores `sql.Conn` fijados simultaneamente.

### 3.3 Runner y schema v1

El runner es propio y usa SQL embebido en el binario. Cada migracion tiene version
entera creciente y checksum SHA-256 calculado sobre sus bytes embebidos. Se ejecuta en
una transaccion `BEGIN IMMEDIATE` que crea los objetos, registra version, checksum y
timestamp RFC3339 UTC, y hace rollback completo ante cualquier error.

Antes de aplicar una migracion, el runner:

- rechaza una version aplicada cuyo checksum no coincida;
- rechaza versiones futuras;
- omite una version ya aplicada y valida, sin cambiar su timestamp ni otros datos; y
- aplica pendientes en orden, una transaccion por migracion.

La migracion v1 crea:

- `schema_migrations`, con `version`, `checksum` y `applied_at`;
- metadata de instancia, incluyendo el envelope verificador de clave; y
- administradores con `id`, `username`, `password_hash`, `created_at`, `updated_at`,
  `password_changed_at`, `disabled_at` nullable y `version`.

En una DB nueva, el schema v1 y el verificador se escriben en la misma transaccion: no
puede quedar una DB inicializada sin verificador. El singleton se implementa
exactamente como indice de expresion parcial:

```sql
CREATE UNIQUE INDEX administrators_one_active
ON administrators ((1))
WHERE disabled_at IS NULL;
```

Asi dos inserts concurrentes con usernames distintos tampoco pueden crear dos
administradores activos.

## 4. Clave y cifrado

`master_key_path` debe existir. En Linux runtime lo abre atomicamente mediante
`golang.org/x/sys/unix.Open` con `O_RDONLY|O_CLOEXEC|O_NOFOLLOW|O_NONBLOCK`, envuelve ese
descriptor y ejecuta `fstat` sobre el mismo archivo abierto. `fstat` es la autoridad:
exige archivo regular, modo exactamente `0600` y tamano 32; luego lee de ese mismo
descriptor exactamente 32 bytes seguidos por EOF. `O_NONBLOCK` evita que un FIFO bloquee
el proceso antes de que `fstat` lo rechace. No acepta symlinks, base64, hexadecimal ni
salto final. Verificar propietario corresponde al instalador y al futuro comando
`verify`, no a este chequeo runtime. Los comandos no crean ni reemplazan la clave.

Los secretos reutilizables usan AES-256-GCM. El encoding persistente del envelope v1 es:

- byte 0: `0x01`;
- bytes 1..12: nonce aleatoria de 12 bytes; y
- bytes 13 en adelante: ciphertext seguido por el tag GCM.

La AAD concatena los bytes ASCII `atalaya\x00secret\x00v1\x00`, el largo en bytes del
tipo como `uint16` big-endian, el tipo UTF-8, el largo en bytes del ID como `uint16`
big-endian y el ID UTF-8. Descifrar exige los mismos tipo e ID; versiones desconocidas,
campos que excedan `uint16`, envelopes truncados, AAD incorrecta o ciphertext alterado
fallan sin devolver plaintext.

El verificador usa tipo `instance`, ID `master-key-check` y plaintext ASCII
`atalaya-master-key-check-v1`. Su descifrado prueba la clave sin exponer detalles
criptograficos. Un golden test fija los bytes de envelope/AAD y una prueba de
compatibilidad descifra un vector persistido conocido.

## 5. Password y administrador

### 5.1 Argon2id

El formato v1 es PHC estricto con Argon2id v19:

```text
$argon2id$v=19$m=65536,t=3,p=4$<salt-base64-sin-padding>$<hash-base64-sin-padding>
```

Se usan `m=65536` KiB, `t=3`, `p=4`, salt CSPRNG de 16 bytes y hash de 32 bytes. El
parser solo acepta algoritmo, version, orden de parametros, valores y longitudes v1;
rechaza campos extra, base64 invalido y parametros que puedan provocar consumo de
recursos no acotado. La comparacion final es constante. Estos parametros corresponden
a la segunda opcion recomendada para entornos con memoria limitada de
[RFC 9106](https://www.rfc-editor.org/rfc/rfc9106.html).

### 5.2 `admin create`

Si `--username` se omite, se usa `admin`. El normalizador aplica `TrimSpace`, acepta
mayusculas ASCII convirtiendolas a minusculas y rechaza cualquier otro caracter no
ASCII o de control. El resultado debe cumplir
`[a-z0-9][a-z0-9._-]{0,63}` y es el valor persistido. Login debe reutilizar exactamente
el mismo normalizador.

El password nunca se acepta por argv, entorno o stdin redirigido: el comando requiere
TTY, la solicita dos veces sin eco mediante `golang.org/x/term` y compara los bytes
exactos. Debe ser UTF-8 valido y tener entre 12 y 256 bytes. No se hace trim ni se
imponen reglas de composicion. Un mismatch o cualquier validacion fallida ocurre antes
de abrir una conexion writable; los buffers se limpian en cuanto sea razonablemente
posible.

La transaccion de insert del administrador, separada de las migraciones, persiste:

- ID UUIDv4 generado con CSPRNG;
- username normalizado y hash PHC, nunca password reversible;
- `created_at`, `updated_at` y `password_changed_at` iguales, en RFC3339 UTC;
- `disabled_at = NULL`; y
- `version = 1`.

El comando consulta primero si ya existe un administrador activo para entregar un error
claro, y conserva el indice parcial como garantia final. Los tests usan reloj y fuente
de aleatoriedad inyectables donde corresponda.

`golang.org/x/term v0.45.0` y `golang.org/x/sys v0.47.0` pasan a ser dependencias
directas y se registran con sus licencias en `docs/dependencies.md` junto al cambio de
codigo.

## 6. Integracion, documentacion y pruebas

`make dev-server` acepta `CONFIG=<path>` y lo reenvia como `--config`; README documenta
el flujo y la generacion manual de una clave raw `0600`. La generacion automatica queda
reservada al instalador futuro.

`make test-e2e` crea configuracion, clave `0600` y DB aisladas bajo `build/e2e`, y no
versiona esos artefactos. El servidor debe conservar `/healthz`, los assets embebidos,
el fallback SPA existente y respuestas 404 no HTML bajo `/api/*`.

La cobertura minima incluye:

- YAML valido, campos desconocidos, ausentes, paths relativos, modos/origen/listener y
  redaccion de diagnosticos;
- clave con modo, tamano o tipo incorrecto, symlink, FIFO que no bloquea, reemplazo
  concurrente y lectura exacta de 32 bytes mas EOF desde el mismo descriptor;
- round-trip AES-GCM, nonces distintos, AAD incorrecta, manipulacion y version
  desconocida;
- PHC valido, password correcta/incorrecta, salts distintos, Unicode, limites y
  formatos maliciosos;
- SQLite real temporal: clasificacion fresh/foreign/corrupta/futura, migraciones no
  contiguas, objetos requeridos malformados, repeticion idempotente, rollback y
  checksum alterado;
- preflight con sidecars WAL/SHM presentes y ausentes, vista consistente, clave
  incorrecta, archivo principal byte-identico y filas logicas sin cambios;
- pragmas efectivos en al menos dos conexiones fijadas;
- administrador con UUIDv4, timestamps deterministas, ausencia de plaintext, mismatch
  sin crear DB nueva, fallo post-migracion y dos inserts concurrentes con usernames
  distintos rechazados por el singleton;
- golden/compatibilidad del encoding persistente AES-GCM y sentinel exacto;
- ayuda y codigos de salida de los tres comandos, y ausencia de listener ante cualquier
  fallo de bootstrap; y
- smoke E2E de `/healthz`, assets, fallback SPA y 404 de API.

La verificacion final ejecuta `make lint`, `make test`, `make build` y
`make test-e2e`. Los builds Linux `amd64` y `arm64` continúan usando
`CGO_ENABLED=0`.

## 7. Supuestos y fuera de alcance

- Los directorios padre, YAML y clave ya fueron aprovisionados; estos comandos no los
  crean.
- No se agrega comando generador de clave ni compatibilidad hibrida con los flags de
  Fase 0.
- Arrancar sin administrador es valido, pero nunca implica crearlo automaticamente.
- La UI bootstrap sigue publica porque aun no presenta datos persistentes.
- `admin reset-password`, login, sesiones, cookies, CSRF, cambio de password y cambios
  Vue autenticados quedan fuera de esta entrega.

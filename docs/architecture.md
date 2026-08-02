# Arquitectura

## 1. Stack

### Runtime

- Go estable soportado por el proyecto para servidor, agente y CLI.
- SQLite con claves foraneas y modo WAL.
- Un ejecutable Linux para `amd64` y `arm64`.
- `systemd` para supervisar servidor y agentes.
- Tailscale para conectividad privada y resolucion MagicDNS.

Builds fijan Go `1.26.5` y Node.js LTS `24.18.0` mediante `.tool-versions`; npm queda
fijado en `11.16.0` mediante `packageManager` y el lockfile. Tailscale es una
dependencia del sistema validada por capacidades: cada release
prueba en CI la version estable vigente y la version mas antigua incluida en su matriz
de compatibilidad, sin prometer un minimo global independiente de la release.
El módulo Go canónico es `github.com/mkrebs03/atalaya`. La integración continua corre
en GitHub Actions: lint, tests, build y la matriz Tailscale por release.

Dependencias Go fundacionales aprobadas para v1:

- `modernc.org/sqlite`, driver SQLite pure Go para conservar builds
  `CGO_ENABLED=0` y cross-compilation sin toolchain C.
- `golang.org/x/crypto/argon2` para passwords Argon2id.
- `golang.org/x/sys/unix` para controles Linux de archivos y terminales sensibles.
- `golang.org/x/term` para detectar la terminal local usada al ingresar passwords.
- `go.yaml.in/yaml/v3` para los archivos de configuracion YAML.

SQLite, Argon2id y YAML son dependencias fundacionales; `x/sys` y `x/term` respaldan
la persistencia, la configuración, la clave maestra y la creación del administrador.
Una prueba de compatibilidad continua ejercita SQLite en memoria, Argon2id y
decodificación YAML estricta con `CGO_ENABLED=0`; el build también la compila, sin
ejecutarla, para Linux `amd64` y `arm64`.

El resto usa biblioteca estandar salvo que una dependencia nueva reduzca riesgo o
complejidad de forma demostrable. Toda dependencia directa queda fijada en `go.mod` y
revisada junto con su licencia y soporte de las dos arquitecturas objetivo.

### Frontend

- Vue 3 con Composition API.
- TypeScript en modo estricto.
- Vite para desarrollo y build.
- Vue Router para navegacion.
- `uPlot` para series temporales.
- CSS propio; no framework visual en v1.
- Pinia solo si aparece estado global no resuelto limpiamente con composables.

Vite produce `web/dist` antes de cualquier test, vet o build Go que requiera el paquete
`web`; Go lo incluye con `go:embed` y sirve UI y API desde el mismo origen. Node.js no
forma parte del runtime ni del instalador. La instalación npm usa un stamp derivado del
manifiesto, lockfile y toolchain; no usa hooks de lifecycle para modificar el árbol de
dependencias.

## 2. Topologia v1

```text
Navegador dentro de la tailnet
       |
       | HTTPS / Tailscale Serve
       v
+------------- Atalaya server host --------+
| atalaya server                             |
|  - assets Vue                              |
|  - API JSON                                |
|  - scheduler / collector                   |
|  - checks de servicio                      |
|  - evaluador de alertas                    |
|  - dispatcher de notificaciones            |
|  - SQLite                                  |
|                                            |
| atalaya agent <---- pull autenticado ------+
+--------------------------------------------+
       |
       | pull autenticado por la tailnet
       v
+--------------- Host Linux remoto ----------+
| agente Atalaya                             |
+--------------------------------------------+
```

Tailscale Serve termina HTTPS del dashboard en puerto 443. Cada agente escucha HTTP
solo en `127.0.0.1:9474`; Tailscale Serve del host publica ese listener mediante HTTPS
en puerto 9474. El servidor consulta `https://<magicdns>:9474`. En el host del
servidor ambas rutas coexisten: dashboard en 443 y agente en 9474. Certificados y
renovación pertenecen a Tailscale Serve, no al proyecto.

El puerto HTTPS 9474 es fijo en v1 y `agent_url` acepta exclusivamente
`https://<fqdn>.ts.net:9474`. El
instalador verifica que la CLI instalada pueda publicar simultaneamente HTTPS 443 y
9474, configura ambas rutas y confirma el estado efectivo. Si falta una capacidad,
falla mostrando version detectada, operacion no soportada y matriz de compatibilidad de
la release, sin cambiar silenciosamente de puerto.

El instalador configura ambas rutas cuando instala server (que incluye el agente local)
y solamente 9474 para un host agent-only; el target del dashboard se deriva del listener
loopback configurado. La sintaxis exacta de CLI se adapta a la version de Tailscale
detectada y se prueba antes de guardar config. Trafico HTTP sin cifrar queda
limitado a loopback; el acceso HTTPS publicado se autoriza mediante ACLs/grants de la
tailnet.
Instalar el rol server instala también el agente local y genera directamente su
credencial; no usa un token de UI ni requiere una segunda instalación manual.

## 3. Responsabilidades

### Servidor

- Servir frontend y API de mismo origen.
- Autenticar administrador y mantener sesiones.
- Administrar configuracion persistente.
- Emitir y consumir tokens de enrolamiento.
- Programar pulls sin ejecutar dos pulls simultaneos para mismo host.
- Validar y persistir snapshots.
- Ejecutar checks HTTP/TCP.
- Agregar y retener series.
- Evaluar reglas e incidentes.
- Despachar notificaciones fuera de transaccion principal.
- Exponer salud del proceso sin datos sensibles.

### Agente

- Leer metricas Linux sin privilegios elevados siempre que sea posible.
- Consultar CPU, memoria, uptime, filesystems y estado local de Tailscale.
- Exponer un snapshot versionado y autenticado.
- Enrolarse y guardar credencial con permisos `0600`.
- No persistir historico ni evaluar alertas en v1.
- No ejecutar comandos enviados por servidor.
- No aceptar scripts, paths arbitrarios ni plugins desde UI.

Se inicia con `atalaya agent --config <path>`. Su YAML estricto contiene `listen`,
`server_url`, `advertised_url` y `credential_path`; paths relativos se resuelven desde
el directorio del YAML.

### Frontend

- Consumir exclusivamente `/api/v1` de mismo origen.
- Tratar servidor como fuente de verdad.
- Mostrar loading, empty, stale, unknown y error como estados distintos.
- No almacenar credenciales o secretos en `localStorage`/`sessionStorage`.
- Actualizar resumen periodicamente; pausar polling cuando pagina no sea visible es
  aceptable.

## 4. Modulos internos sugeridos

```text
cmd/atalaya/                 CLI y seleccion de comandos
internal/agent/              HTTP del agente y recoleccion Linux
internal/auth/               passwords, sesiones y CSRF
internal/config/             config de arranque
internal/crypto/             cifrado de secretos
internal/database/           SQLite, migraciones y queries
internal/collection/         scheduler y pulls
internal/checks/             HTTP y TCP
internal/alerts/             reglas, estados e incidentes
internal/notifications/      interfaz y adaptador Discord
internal/httpapi/            routes, DTOs y middleware
internal/retention/          rollups y limpieza
internal/server/             composicion del proceso
web/                         aplicacion Vue
scripts/                     instalacion y release
```

Limites importan mas que nombres exactos. Paquetes de dominio no deben importar capa
HTTP ni componentes Vue. Adaptadores externos implementan interfaces pequenas dentro
del modulo que los consume.

La operación de release agrega tres piezas operativas:

- internal/backup valida el estado vigente, crea el snapshot SQLite con VACUUM INTO y
  empaqueta manifest, base y configuracion no secreta; la clave maestra se exporta por
  separado.
- internal/tailscale concentra la deteccion de arquitectura, version, estado conectado,
  Self.DNSName y las dos capacidades Serve. El instalador solo invoca ese adaptador y
  no conoce variantes de la CLI.
- scripts/install.sh descarga y verifica el binario, instala usuario, paths y units
  systemd, y ejecuta el flujo de upgrade con snapshot pre-migracion, rename atomico,
  healthcheck y rollback.

El binario sigue siendo unico: server y agent comparten ejecutable, pero cada unit
recibe su YAML. El estado operativo autenticado se calcula desde SQLite sin agregar
tablas ni una migracion nueva. La release de GitHub contiene ambos binarios Linux,
SHA256SUMS, el instalador y las notas de la matriz Tailscale.

## 5. Flujo de enrolamiento

1. Para un host remoto, el administrador crea token desde UI. v1 permite exactamente un enrolamiento y 15
   minutos de validez.
2. UI muestra token una sola vez.
3. Operador instala agente indicando URL del servidor y nombre MagicDNS local. El
   token se ingresa por prompt interactivo o archivo `0600` que se elimina tras el
   enrolamiento; nunca por argumento de linea de comandos.
4. Agente conecta por HTTPS a `POST /api/v1/agent-enrollments`.
5. Servidor valida token, expiracion y uso dentro de una transaccion.
6. Servidor crea host pendiente y credencial aleatoria por agente.
7. Servidor guarda credencial cifrada; agente la recibe una sola vez y la guarda con
   permisos restringidos.
8. Agente guarda credencial atomicamente y deja listener disponible.
9. Scheduler incluye hosts `pending` y `active`, e intenta
   `GET /agent/v1/snapshot` usando esa credencial.
10. Primer pull valido cambia host de `pending` a observado.

En el host del servidor, `atalaya bootstrap local-agent` crea de forma idempotente el
host, credencial cifrada y configuración del agente. Una primera transacción crea o recupera
el unico `source=local` incompleto; despues se escriben credencial y YAML atomicamente,
y una segunda transaccion fija `bootstrap_completed_at` y agenda el primer slot. El
host incompleto no es visible ni schedulable. El
instalador server reutiliza ese comando y despues instala
`atalaya-agent.service`. El primer pull usa el mismo protocolo y cambia el host a
observado.

La interfaz completa del comando es `atalaya bootstrap local-agent --server-config
<path> --agent-config <path> --agent-url <url> [--display-name <name>] [--listen
<addr>] [--credential-path <path>]`. `server_url` deriva de `public_url` del server
config y `advertised_url` es exactamente `--agent-url` tras validarlo. Se escribe
primero la credencial `0600` y luego la config `0640`, mediante temporales en el mismo
directorio, fsync y rename sin sobrescritura. Temporales se limpian al fallar. Un
archivo existente valido se verifica y nunca se reemplaza; uno ausente se crea. Si
falla la config despues de crear la credencial, esa credencial valida y la
identidad/secreto en DB permanecen y el rerun completa el YAML.

Credential canonica es base64url sin padding de 32 bytes (43 caracteres) mas LF; al
verificar se tolera solo ausencia del LF final y se compara el secreto decodificado en
tiempo constante. Config se compara semanticamente tras YAML estricto de documento
unico: listen textual validado, URLs canonicas y credential path absoluto limpio;
orden/comentarios no cuentan. CR, whitespace/padding extra, YAML desconocido,
multi-documento, permisos distintos o valores diferentes fallan sin reescritura.

Reintentar una respuesta perdida no debe consumir varios hosts. Peticion de
enrolamiento incluye `request_id` aleatorio persistido por agente para idempotencia.
Solo replay con mismo token, `request_id` y payload puede recuperar respuesta; cualquier
otra reutilizacion se rechaza. Replay exacto se conserva 15 minutos desde consumo,
incluso si token expira durante esa ventana; despues responde token consumido sin
devolver credencial.

## 6. Flujo de recoleccion

1. Scheduler reclama con lease el `ScheduleSlot` no terminal del host habilitado cuyo
   `not_before_at` vencio.
2. La restriccion unica por target impide dos slots no terminales y el lease impide
   pulls concurrentes dentro del proceso.
3. Consulta snapshot con defaults de intervalo 60 segundos, timeout 10 segundos,
   jitter 0-5 segundos, lease de timeout mas 5 segundos, concurrencia global 4 y body
   maximo 256 KiB.
4. Valida version, identidad, timestamp razonable, warnings, tipos y limites de
   payload. Clock skew absoluto mayor a cinco minutos degrada sin invalidar el resto.
5. En una transaccion actualiza estado actual, completa el slot y, si el target sigue
   habilitado, crea exactamente el siguiente slot de la cadencia nominal con jitter
   pequeño. La persistencia de muestras crudas comparte esta frontera transaccional.
6. Libera el lease despues del commit.
7. Evaluador procesa reglas relacionadas despues de persistencia.

Scheduler tambien materializa la transicion stale al llegar
`freshness_deadline_at`, siempre que snapshot/host sigan vigentes. Pull, cambios de
snapshot/filesystems/warnings/estado, stale y unreachable incrementan `Host.version`;
el detalle puede usarlo como strong ETag y un futuro `If-Match` puede perder contra el
polling con 412.

Cada slot programado captura `effective_interval_seconds`,
`effective_timeout_seconds`, `nominal_at`, `not_before_at` y `deadline_at` al crearse;
cambios posteriores no alteran ese intento ni los deadlines ya calculados. El slot y
su lease se persisten antes de iniciar red. Un fallo operativo programado completado
conserva ultimo snapshot e incrementa fallos consecutivos. Un `FailureProtocol` solo
cierra el slot y crea el siguiente nominal: no cambia estado, version, contadores,
timestamps, snapshot o filesystems. Intentos manuales, schedules omitidos durante downtime e interrupciones
por shutdown no cuentan. Host observado pasa a `unreachable` solo cuando acumula tres
fallos operativos programados y se alcanza el deadline del tercer slot fallido; cada slot usa el
intervalo que capturo al programarse. Antes queda `degraded` solo si ya fue observado;
un host nunca observado permanece `unknown` tras uno o dos fallos y hasta el deadline
del tercer fallo. Su primer slot parte del enrolamiento. UI muestra edad del dato;
nunca presenta valor anterior como actual sin advertencia.

Despues de un exito o fallo completado dentro de su deadline, el scheduler crea el
siguiente slot con
`nominal_at = previous.nominal_at + previous.effective_interval`, usando la
configuracion vigente en esa frontera. Solo un exito actualiza
`freshness_deadline_at = next_slot.deadline_at + 5 segundos`; fallos, downtime y
reanclajes no prolongan la frescura del ultimo valor. Para un check, exito del slot es
haber producido resultado `up` o `down`: un check caido sigue entregando datos frescos
y no se presenta como stale. `last_success_received_at` siempre
usa `received_at` del servidor, nunca `observed_at` del agente; es el timestamp de la
muestra, no el ancla de la cadencia. Cambiar intervalo aplica al primer slot que se cree
despues del cambio; no modifica un slot ya creado ni recalcula frescura o timestamps
historicos.

Tras restart, un slot `running` con lease vencido vuelve a `scheduled`. Si `now` no
supera su `deadline_at` y el target sigue habilitado, se reintenta una sola vez
conservando `slot_id`. Si el deadline ya paso, no inicia red: lo cancela con razon
`missed_deadline`, cierra el periodo en
`min(now, expired.nominal_at + effective_interval)` y abre un periodo con un slot
inmediato anclado en `now` y la configuracion vigente.
Slots nominales del hueco no se materializan, no cuentan como fallos ni como muestras
esperadas. El slot vencido ya emitido permanece esperado y deja un hueco de cobertura.
Resultado, finalizacion y siguiente slot comparten transaccion, y muestras/resultados
son unicos por `slot_id`, por lo que un retry tras crash no duplica historico ni deja el
target sin agenda.

Un shutdown no convierte por si solo un intento en fallo terminal: libera el lease o
permite que venza para aplicar la recuperacion anterior. Cancelaciones administrativas
persisten razon `target_disabled` o `target_archived`.

La transaccion de finalizacion vuelve a leer el estado administrativo. Si el target se
desactivo o archivo durante la red, marca el slot `cancelled`, descarta ese resultado
para estado/series/alertas y no crea el siguiente slot. Asi una operacion in-flight no
revive scheduling despues de una accion administrativa.

Scheduler v1 vive en un proceso. No se diseña coordinacion distribuida porque SQLite y
alta disponibilidad estan fuera de alcance.

## 7. Checks de servicios

Checks se ejecutan en servidor, independientemente del pull de agentes.

- HTTP sigue redirects solo si configuracion lo permite; estan desactivados por default
  y el limite es configurable de 1 a 3, con tope duro de 3.
- HTTPS valida certificados del sistema; desactivar validacion queda fuera de v1.
- Respuesta se considera exitosa por rango HTTP configurado.
- Cuerpo de respuesta no se persiste.
- TCP mide tiempo hasta conexion establecida y cierra inmediatamente.
- DNS y conexion usan timeout total del check.
- Errores persistidos usan categorias estables: `dns`, `timeout`, `connect`, `tls`,
  `http_status`, `invalid_target`, `internal`.

Targets se validan al guardar y antes de cada conexion. Servidor solo permite destinos
Tailscale o CIDRs privados autorizados; IPs publicas quedan bloqueadas por default.
Todas las IPs DNS deben cumplir la politica, y cada redirect se resuelve y valida de
nuevo.

## 8. Procesamiento asincrono

No se agrega broker externo. Trabajo asincrono usa workers acotados y tablas SQLite:

- Pulls y checks programados usan `ScheduleSlot` persistente.
- Checks manuales usan `CheckRun` persistente con claim y lease recuperable.
- Notificaciones usan outbox persistente para sobrevivir reinicios.
- Rollups y retencion usan jobs idempotentes con marca de progreso.

No mantener transaccion SQLite abierta durante peticiones de red.

## 9. Shutdown y errores

- `SIGTERM` deja de aceptar trabajo nuevo, completa transacciones cortas y cierra
  dentro del timeout de `systemd`.
- Errores esperados se traducen a categorias estables de API.
- Panics en request no derriban proceso; se recuperan, registran y responden 500.
- Panics en invariantes de startup deben terminar proceso para que `systemd` lo
  reporte, evitando ejecucion parcialmente inicializada.

## 10. Decisiones deliberadamente diferidas

- Watchdog en segundo host.
- WebSockets o Server-Sent Events; polling basta para v1.
- Multiusuario.
- Base de datos externa.
- Agentes con buffer local.
- Sistema general de plugins.
- Separar binarios de servidor y agente.

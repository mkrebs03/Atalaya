# Arquitectura

## 1. Stack

### Runtime

- Go estable soportado por el proyecto para servidor, agente y CLI.
- SQLite con claves foraneas y modo WAL.
- Un ejecutable Linux para `amd64` y `arm64`.
- `systemd` para supervisar servidor y agentes.
- Tailscale para conectividad privada y resolucion MagicDNS.

Builds fijan Go `1.26.4` y Node.js LTS `24.18.0` mediante `.tool-versions` y un lockfile
npm. La version de Tailscale es una dependencia del sistema: el instalador detecta
capacidades de la CLI y la version minima soportada se prueba en CI.
El modulo Go canonico es `github.com/mkrebs03/atalaya`; no depende del nombre comercial
que pueda adoptar el producto.

Dependencias Go fundacionales aprobadas para v1:

- `modernc.org/sqlite`, driver SQLite pure Go para conservar builds
  `CGO_ENABLED=0` y cross-compilation sin toolchain C.
- `golang.org/x/crypto/argon2` para passwords Argon2id.
- `go.yaml.in/yaml/v3` para los archivos de configuracion YAML.

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

Vite produce assets estaticos durante build. Go los incluye con `go:embed` y sirve UI
y API desde mismo origen. Node.js no forma parte del runtime ni del instalador.

## 2. Topologia v1

```text
Browser in tailnet
       |
       | HTTPS / Tailscale Serve
       v
+---------------- Oracle VM ----------------+
| atalaya server                             |
|  - Vue assets                              |
|  - JSON API                                |
|  - scheduler / collector                   |
|  - service checker                         |
|  - alert evaluator                         |
|  - notification dispatcher                |
|  - SQLite                                  |
|                                            |
| atalaya agent <---- authenticated pull ----+
+--------------------------------------------+
       |
       | authenticated pull over tailnet
       v
+-------------- Other Linux host ------------+
| atalaya agent                              |
+--------------------------------------------+
```

Tailscale Serve termina HTTPS del dashboard en puerto 443. Cada agente escucha HTTP
solo en `127.0.0.1:9474`; Tailscale Serve del host publica ese listener mediante HTTPS
en puerto 9474. El servidor consulta `https://<magicdns>:9474`. En Oracle ambas rutas
coexisten: dashboard en 443 y agente en 9474. Certificados y renovacion pertenecen a
Tailscale Serve, no al proyecto.

El puerto HTTPS 9474 es el default fijo de v1 y queda registrado en `agent_url`. El
instalador verifica que la CLI instalada admita `serve --https=9474`, configura la
ruta y comprueba el estado efectivo; una version incompatible falla con la version
minima requerida, sin cambiar silenciosamente de puerto.

El instalador configura ambas rutas cuando instala roles juntos y verifica que el
listener local no este ocupado. La sintaxis exacta de CLI se adapta a la version de
Tailscale detectada y se prueba antes de guardar config. Trafico HTTP sin cifrar queda
limitado a loopback; el acceso HTTPS publicado se autoriza mediante ACLs/grants de la
tailnet.
Instalar el rol server en Oracle instala tambien el agente local y genera directamente
su credencial; no usa un token de UI ni requiere una segunda instalacion manual.

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

## 5. Flujo de enrolamiento

1. Para un host remoto, el administrador crea token desde UI. v1 permite exactamente un enrolamiento y 15
   minutos de validez.
2. UI muestra token una sola vez.
3. Operador instala agente indicando URL del servidor, token y nombre MagicDNS local.
4. Agente conecta por HTTPS a `POST /api/v1/agent-enrollments`.
5. Servidor valida token, expiracion y uso dentro de una transaccion.
6. Servidor crea host pendiente y credencial aleatoria por agente.
7. Servidor guarda credencial cifrada; agente la recibe una sola vez y la guarda con
   permisos restringidos.
8. Agente guarda credencial atomicamente y deja listener disponible.
9. Scheduler incluye hosts `pending` y `active`, e intenta
   `GET /agent/v1/snapshot` usando esa credencial.
10. Primer pull valido cambia host de `pending` a observado.

Para Oracle, `atalaya bootstrap local-agent` crea de forma idempotente el host,
credencial cifrada y configuracion del agente dentro de una operacion local. El
instalador server reutiliza ese comando y despues instala
`atalaya-agent.service`. El primer pull usa el mismo protocolo y cambia el host a
observado.

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
3. Consulta snapshot con timeout menor que intervalo y jitter maximo de 5 segundos.
4. Valida version, identidad, timestamp razonable, tipos y limites de payload.
5. En una transaccion actualiza estado actual, agrega muestras crudas con `slot_id`,
   completa el slot y, si el target sigue habilitado, crea exactamente un siguiente
   slot futuro con jitter pequeno.
6. Libera el lease despues del commit.
7. Evaluador procesa reglas relacionadas despues de persistencia.

Cada slot programado captura `effective_interval_seconds`,
`effective_timeout_seconds`, `nominal_at`, `not_before_at` y `deadline_at` al crearse;
cambios posteriores no alteran ese intento ni los deadlines ya calculados. El slot y
su lease se persisten antes de iniciar red. Un fallo programado
completado conserva ultimo snapshot e incrementa fallos
consecutivos. Intentos manuales, schedules omitidos durante downtime y cancelaciones
por shutdown no cuentan. Host observado pasa a `unreachable` solo cuando acumula tres
fallos programados y se alcanza el deadline del tercer slot fallido; cada slot usa el
intervalo que capturo al programarse. Antes queda `degraded`. Para host nunca
observado, el primer slot parte del enrolamiento. UI muestra edad del dato; nunca
presenta valor anterior como actual sin advertencia.

Despues de un exito, el scheduler crea el siguiente slot con la configuracion vigente.
Snapshot/check es fresh hasta el `deadline_at` de ese siguiente slot mas 5 segundos de
margen operativo; equivalentemente,
`last_success_received_at + effective_interval + effective_timeout + jitter_max + 5
segundos`, con `jitter_max` de 5 segundos. Despues es stale.
`last_success_received_at` siempre usa `received_at` del servidor, nunca `observed_at`
del agente. Cambiar intervalo aplica al primer slot que se cree despues del cambio; no
modifica un slot ya creado ni recalcula frescura o timestamps historicos.

Tras restart, un slot `running` con lease vencido vuelve a `scheduled`. Si su
`not_before_at` ya paso y el target sigue habilitado, se ejecuta una sola vez de forma
inmediata conservando `slot_id`; slots nominales intermedios durante downtime no se
materializan ni cuentan como fallos. Resultado, finalizacion del slot y creacion del
siguiente comparten transaccion, y muestras/resultados son unicos por `slot_id`, por lo
que un retry tras crash no duplica historico ni deja el target sin agenda. El siguiente
slot se ancla en el momento actual, sin ejecutar una cascada de catch-up.

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

- Pulls y checks pueden reintentarse desde scheduler en memoria.
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

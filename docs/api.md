# API y protocolos

## 1. Convenciones

- API de UI: `/api/v1` sobre mismo origen que frontend.
- API expuesta por agentes: `/agent/v1`.
- JSON UTF-8 y `Content-Type: application/json`.
- Nombres JSON en `snake_case`.
- Timestamps RFC 3339 UTC.
- IDs opacos.
- Mutaciones UI requieren sesion y proteccion CSRF. Excepciones documentadas:
  `POST /api/v1/auth/login` (pre-auth, valida `Origin`, sin CSRF) y
  `POST /api/v1/agent-enrollments` (usado por agentes, autentica con token de
  enrolamiento, sin sesion ni CSRF, rate-limited).
- Respuestas nunca incluyen hashes, ciphertexts, tokens reutilizables ni secretos,
  salvo las excepciones efimeras documentadas para crear un token de enrolamiento y
  completar/repetir exactamente un enrolamiento autorizado. Esas respuestas no se
  almacenan en frontend, logs, caches ni errores.
- Todos los endpoints de autenticacion, respuestas que establecen cookies y respuestas
  efimeras con token o credencial envian `Cache-Control: no-store`. Intermediarios no
  deben almacenar contenido autenticado; la API puede aplicar `no-store` a todas sus
  respuestas para mantener una politica uniforme.

Formato de error:

```json
{
  "error": {
    "code": "validation_failed",
    "message": "Request could not be saved",
    "fields": {
      "name": "Name is required"
    },
    "request_id": "opaque-id"
  }
}
```

`message` es seguro para usuario. Detalles internos solo van a logs asociados por
`request_id`.

Codigos comunes:

- `400`: JSON o parametros invalidos.
- `401`: sin sesion/credencial valida.
- `403`: CSRF o accion no permitida.
- `404`: recurso inexistente o no visible.
- `409`: unicidad o transicion en conflicto; conflictos de version usan `412`.
- `412`: `If-Match` no coincide con version actual.
- `422`: estructura valida pero semantica de dominio invalida, sea configuracion o
  parametros de consulta fuera de limites.
- `428`: mutacion de recurso existente sin `If-Match`.
- `429`: login, enrolamiento o test limitado.
- `500`: error interno no revelado.
- `503`: dependencia local temporalmente no disponible.

Listas usan `limit` con maximo y cursor opaco. Respuesta:

```json
{
  "items": [],
  "next_cursor": null
}
```

## 2. Endpoints publicos del servidor

### `GET /healthz`

Indica que proceso puede responder. No consulta agentes ni devuelve version,
hostname, metricas o estado de base detallado.

```json
{"status":"ok"}
```

La SPA solo usa fallback para `GET`/`HEAD` de rutas de interfaz sin apariencia de
archivo. `/api/*`, assets ausentes, paths con segmentos `.` o `..` y paths de archivo
fuera de `/assets/` responden sin HTML; asi un `favicon.ico`, script, manifest o
traversal nunca recibe `index.html`. Assets versionados bajo `/assets/` usan cache
inmutable y el index usa `no-cache`.

### `POST /api/v1/auth/login`

Acepta username y password. Crea cookie de sesion; no devuelve bearer token.
Rate-limited. Error no distingue usuario inexistente de password incorrecto.
Login no exige token CSRF porque todavia no existe sesion. Debe validar `Origin` contra
origen publico configurado y rechazar `Sec-Fetch-Site: cross-site`; navegadores que no
envien Fetch Metadata siguen sujetos a Origin, cookie SameSite y rate limit global.

Request exacto, JSON estricto y maximo 4 KiB:

```json
{"username":"admin","password":"secret"}
```

Login, `GET /api/v1/auth/me` y cambio exitoso responden:

```json
{
  "administrator":{"id":"opaque-id","username":"admin"},
  "session":{"idle_expires_at":"RFC3339","absolute_expires_at":"RFC3339"}
}
```

### `POST /api/v1/agent-enrollments`

Usado por agente, no navegador. Request incluye:

```json
{
  "enrollment_token": "one-time-secret",
  "request_id": "agent-generated-id",
  "display_name": "oracle-vm",
  "agent_url": "https://oracle-vm.tailnet-name.ts.net:9474",
  "agent_version": "semver",
  "protocol_version": 1
}
```

Respuesta exitosa mostrada una vez al agente:

```json
{
  "host_id": "opaque-id",
  "agent_credential": "secret",
  "poll_interval_seconds": 60
}
```

El token es base64url sin padding de 32 bytes, expira en 15 minutos y solo se puede
consumir una vez. Repetir exactamente el mismo token, `request_id` y payload dentro de
la ventana de replay devuelve el mismo resultado. La ventana dura 15 minutos desde el
primer consumo, aunque la expiracion original ocurra antes. Despues, el replay responde
conflicto de token consumido. El request esta limitado a 4 KiB y `agent_url` debe ser
`https://<fqdn>.ts.net:9474`.

## 3. Auth y sesion

- `POST /api/v1/auth/logout`
- `GET /api/v1/auth/me`
- `POST /api/v1/auth/change-password`
- `GET /api/v1/auth/csrf`

Logout y cambio de password requieren CSRF. Cambio de password revoca todas las
sesiones existentes y puede emitir una sesion nueva para el request actual.
`GET csrf` responde `{"csrf_token":"base64url"}`. Logout exitoso responde `204`.
Cambio acepta `{"current_password":"old","new_password":"new"}` y siempre emite
sesion reemplazo al completar. Codigos auth: `invalid_credentials` 401,
`authentication_required`/`session_expired` 401, `current_password_invalid` y
`csrf_failed` 403, `validation_failed` 400/422 y `rate_limited` 429 con
`Retry-After`. Todo error incluye `request_id`.

Todo recurso mutable devuelve `version` entero y header `ETag: "<version>"`. `PATCH`,
`DELETE`, enable/disable y otras mutaciones sobre recurso existente requieren
`If-Match: "<version>"`. Exito incrementa version. Mismatch responde 412 con version
actual; ausencia responde 428. Acciones asincronas que no editan configuracion, como
`run` y `test`, no requieren `If-Match`.

`DELETE` sobre hosts, checks, reglas y canales siempre establece `archived_at`, fija
`enabled=false` y conserva historico, incidentes y entregas. Las listas excluyen
recursos archivados por default. `restore` quita el archivado pero deja `enabled=false`;
reactivar monitoreo o evaluacion requiere una accion `enable` posterior. Los tokens de
enrolamiento usan `revoked_at` y no el ciclo de archivado.

## 4. Hosts y enrolamiento

- `GET /api/v1/hosts`
- `GET /api/v1/hosts/{id}`
- `PATCH /api/v1/hosts/{id}`
- `DELETE /api/v1/hosts/{id}`
- `POST /api/v1/hosts/{id}/restore`
- `POST /api/v1/hosts/{id}/enable`
- `POST /api/v1/hosts/{id}/disable`
- `GET /api/v1/hosts/{id}/filesystems`
- `PATCH /api/v1/hosts/{id}/filesystems/{filesystem_id}`
- `GET /api/v1/hosts/{id}/metrics?from=&to=&resolution=auto`
- `POST /api/v1/enrollment-tokens`
- `GET /api/v1/enrollment-tokens`
- `DELETE /api/v1/enrollment-tokens/{id}`

Fase 2A implementa los endpoints read-only; Fase 2B agrega enrolamiento y mutaciones.
Todos exigen sesion salvo el endpoint de enrolamiento. `limit` tiene default 50 y maximo
100; `cursor` es opaco. Limit/cursor invalido responde 400
`validation_failed` con `fields`, sesion ausente o invalida responde 401
`authentication_required`, y host ausente, oculto o con bootstrap incompleto responde
404 `not_found`.

Hosts usan keyset `ORDER BY display_name COLLATE NOCASE ASC, id ASC`; el cursor codifica
la ultima tupla `(display_name normalizado, id)`. Filesystems usan
`ORDER BY mount_point ASC, id ASC` y cursor equivalente. No hay snapshot aislado entre
paginas: cambios concurrentes pueden mover items; el cliente puede reiniciar desde la
primera pagina para obtener una vista nueva.

### `GET /api/v1/hosts?limit=&cursor=`

```json
{
  "items": [{
    "id": "opaque-id", "display_name": "Oracle",
    "administrative_state": "pending", "observed_state": "unknown", "stale": false,
    "last_attempt_at": null, "last_success_received_at": null,
    "freshness_deadline_at": null, "consecutive_failures": 0,
    "interval_seconds": 60, "timeout_seconds": 10,
    "cpu_used_percent": null, "memory_used_bytes": null,
    "memory_total_bytes": null, "uptime_seconds": null,
    "tailscale": null, "warnings": [], "agent_version": null,
    "protocol_version": null, "most_constrained_filesystem": null,
    "archived": false,
    "version": 1
  }],
  "next_cursor": null
}
```

`last_attempt_at` es null solo mientras nunca hubo intento. `last_success_received_at`
y `freshness_deadline_at` son null mientras nunca hubo snapshot valido. Las cuatro
metricas actuales, `tailscale`, versiones del agente y `most_constrained_filesystem`
son nullable cuando nunca hubo dato valido. Bytes son
enteros JSON no negativos, uptime usa segundos y porcentajes numeros 0-100.
El host local completado comienza `pending/unknown`; su primer snapshot valido cambia
el estado administrativo a `active` y el observado a `healthy` o `degraded`.
`most_constrained_filesystem` considera solo filesystems `selected=true`,
`present=true` y con total valido; es null si ninguno califica. `warnings` es una lista
de codigos string seguros reportados o derivados por servidor, incluido `clock_skew`.

### `GET /api/v1/hosts/{id}`

Devuelve el mismo objeto summary, agrega `agent_url` y `snapshot`, envia
`Cache-Control: no-store` y strong `ETag: "<version>"`. `If-None-Match` es opcional;
si coincide responde 304 sin body y conserva `no-store`.

```json
{
  "id":"opaque-id", "display_name":"Oracle", "administrative_state":"active",
  "observed_state":"degraded", "stale":false,
  "last_attempt_at":"2026-07-17T12:00:00Z",
  "last_success_received_at":"2026-07-17T12:00:00Z",
  "freshness_deadline_at":"2026-07-17T12:01:20Z", "consecutive_failures":0,
  "interval_seconds":60, "timeout_seconds":10,
  "cpu_used_percent":12.5, "memory_used_bytes":1024, "memory_total_bytes":4096,
  "uptime_seconds":12345,
  "tailscale":{"state":"connected","hostname":"oracle-vm.tailnet.ts.net","addresses":["100.64.0.1"]},
  "warnings":["clock_skew"], "agent_version":"dev", "protocol_version":1,
  "most_constrained_filesystem":null, "archived":false,
  "agent_url":"https://oracle-vm.tailnet.ts.net:9474",
  "snapshot":{
    "received_at":"2026-07-17T12:00:00Z", "observed_at":"2026-07-17T11:50:00Z",
    "hostname":"oracle-vm", "boot_id":"opaque-linux-boot-id", "uptime_seconds":12345,
    "cpu_used_percent":12.5, "memory_used_bytes":1024, "memory_total_bytes":4096,
    "tailscale":{"state":"connected","hostname":"oracle-vm.tailnet.ts.net","addresses":["100.64.0.1"]},
    "collection_warnings":[]
  },
  "version":1
}
```

`snapshot` es null si nunca hubo exito. Dentro de snapshot, `boot_id`, uptime, CPU y
ambos campos de memoria son nullable con las mismas unidades; los dos campos de
memoria son null como par. `tailscale` siempre es objeto con el mismo enum y
nullability del wire del agente. `received_at`, `observed_at`, `hostname` y
`collection_warnings` existen en todo snapshot persistido.

### Gestión de hosts y filesystems

`GET /api/v1/hosts?include_archived=true` incluye recursos archivados para la vista
administrativa. Esos hosts llevan `archived=true`; el valor por defecto es `false` y
los mantiene ocultos.

`PATCH /api/v1/hosts/{id}` acepta uno o más campos y exige `If-Match`:

```json
{"display_name":"media-node","interval_seconds":120,"timeout_seconds":10}
```

El intervalo está entre 15 y 3600 segundos; el timeout entre 1 y 60 segundos y debe
ser menor al intervalo. La respuesta devuelve el detalle actualizado y su nuevo ETag.
`DELETE /api/v1/hosts/{id}` archiva lógicamente; `POST /restore` restaura dejando el
host desactivado, `POST /enable` lo vuelve a programar y `POST /disable` cancela su
programación. Todas requieren `If-Match`; archivar responde `204` y las acciones
restantes devuelven el host actualizado.

`PATCH /api/v1/hosts/{id}/filesystems/{filesystem_id}` acepta
`{"selected":true|false}`, exige el `If-Match` de la versión del filesystem y
responde `204`. Todos los endpoints de gestión requieren sesión y CSRF.

### Tokens de enrolamiento

`POST /api/v1/enrollment-tokens` acepta `{"label":"optional label"}` y responde
`201` con `id`, metadatos, `version` y `token`. El secreto aparece solo en esa
respuesta y nunca vuelve a aparecer en `GET /api/v1/enrollment-tokens`, que devuelve
únicamente metadatos. `DELETE /api/v1/enrollment-tokens/{id}` revoca un token y exige
su ETag; no se puede recuperar el secreto.

`version` incrementa ante cualquier cambio visible del detalle: snapshot/pull,
filesystems, warnings, estado observado, transicion stale persistida, transicion
unreachable y futura configuracion. Por ello el ETag es strong. Futuras mutaciones con
`If-Match` pueden competir con un pull y responder 412 por diseno.

La razon interna de cancelacion de schedules admite `missed_deadline`,
`target_disabled`, `target_archived` y `target_changed`; no forma parte actualmente
de las respuestas publicas de hosts.

### `GET /api/v1/hosts/{id}/filesystems?limit=&cursor=`

```json
{
  "items": [{
    "id":"opaque-filesystem-id", "identity_kind":"uuid", "mount_point":"/",
    "device":"/dev/sda1", "mount_root":"/", "major_minor":"8:1", "fs_type":"ext4",
    "selected":true, "present":true, "used_bytes":1024, "total_bytes":4096,
    "available_bytes":3072, "last_observed_at":"2026-07-17T12:00:00Z", "version":1
  }],
  "next_cursor": null
}
```

`identity_kind` es `uuid|fallback`. Mount point, device, mount root, major:minor y
tipo son strings no vacios. Los tres campos byte y `last_observed_at` son nullable si
no existe observacion valida; bytes son enteros no negativos. La lista conserva
filesystems ausentes con `present=false`. Descubrimiento excluye exactamente la lista
cerrada de `fs_type` definida en `spec.md` y el modelo de dominio.

Crear token devuelve el secreto una vez en la respuesta autenticada. Listarlo despues
solo muestra metadata, estado y expiracion. Esta es una excepcion a la regla general de
secretos y el frontend solo lo conserva en memoria mientras el administrador lo copia.

`DELETE /api/v1/hosts/{id}` siempre archiva y desactiva el host, conserva historico e
incidentes y requiere confirmacion explicita desde UI. `restore` quita el archivado,
pero no reactiva automaticamente el monitoreo si el administrador lo habia desactivado.
`enable` devuelve el host a `pending` cuando nunca tuvo un pull exitoso y a `active`
en caso contrario; el estado observado se recalcula con el siguiente pull.

Respuesta de metricas incluye resolucion real y series alineables sin inventar puntos.
`resolution` solicitado puede ser `auto`, `raw` u `hourly`; la respuesta devuelve
`raw`, `hourly` o `mixed`. `mixed` solo aparece cuando `auto` combina fuentes. Cada
punto declara su propia `resolution`, por lo que el contrato sigue siendo inequivoco
al cruzar la frontera de retencion. Los puntos horarios representan
`[timestamp, timestamp + 1h)`:

```json
{
  "from": "2026-07-14T00:00:00Z",
  "to": "2026-07-15T00:00:00Z",
  "resolution": "hourly",
  "series": [
    {
      "key": "cpu_used_percent",
      "unit": "percent",
      "points": [
        {
          "timestamp": "2026-07-14T00:00:00Z",
          "resolution": "hourly",
          "value": 12.5,
          "min": 8.1,
          "max": 18.4,
          "sample_count": 58,
          "covered_slot_count": 59,
          "expected_sample_count": 60,
          "complete": false
        }
      ]
    }
  ]
}
```

Para puntos `raw`, `min` y `max` igualan `value`, `sample_count` vale 1 y los campos de
cobertura pueden omitirse. Para puntos `hourly`, valores y `sample_count` agrupan por
`received_at`; `expected_sample_count` cuenta slots por `nominal_at` y
`covered_slot_count` cuenta cuales produjeron esa serie mediante `slot_id`. Por ello
`sample_count` puede diferir de ambos cerca de una frontera horaria. `complete` solo es
true en una hora cerrada cuando todos los slots esperados estan cubiertos; nunca se
calcula comparando `sample_count` con `expected_sample_count`.

Ventana sin muestras no produce punto. En `auto`, cada bucket se obtiene de una sola
fuente: rollup para ventanas completamente fuera de la ventana raw y raw para el tramo
reciente. UI no une como continuos puntos separados por una ventana ausente y distingue
visualmente rollup parcial. No usar cero o `null` sintetico como medicion.

`raw` exige que toda la ventana solicitada este dentro de la retencion disponible; si
`from` es anterior al cutoff responde `422` con codigo `raw_not_available` y
`earliest_available_at`, sin devolver datos parciales. `hourly` usa rollups persistidos
y, para una hora abierta o completa aun no agregada que conserve raw, calcula el bucket
en lectura sin persistirlo. Ese punto sigue declarando `resolution=hourly` y
`complete=false` mientras la hora este abierta. Una hora cerrada aun no agregada se
calcula igual en lectura y declara `complete` segun su cobertura real. Si una ventana
no tiene rollup ni raw, permanece como hueco.

El limite es 10000 puntos por serie despues de seleccionar resolucion y combinar
fuentes. Si cualquier serie lo supera, la consulta completa responde `422` con codigo
`point_limit_exceeded`; no devuelve una respuesta truncada. El cliente debe acotar el
rango o solicitar `hourly`.

## 5. Checks

- `GET /api/v1/checks`
- `POST /api/v1/checks`
- `GET /api/v1/checks/{id}`
- `PATCH /api/v1/checks/{id}`
- `DELETE /api/v1/checks/{id}`
- `POST /api/v1/checks/{id}/enable`
- `POST /api/v1/checks/{id}/disable`
- `POST /api/v1/checks/{id}/restore`
- `POST /api/v1/checks/{id}/run`
- `GET /api/v1/checks/{id}/runs?from=&to=&cursor=`
- `GET /api/v1/checks/{id}/runs/{run_id}`
- `GET /api/v1/checks/{id}/results?from=&to=&resolution=auto`

`run` recibe `{ "request_id": "client-generated-opaque-id" }`, programa ejecucion y
devuelve `202` con `{ "run_id": "id", "status": "queued" }`; no mantiene request HTTP
abierto esperando red destino. Repetir el mismo `request_id` para el mismo check
devuelve el mismo `run_id` sin crear otra ejecucion. El resultado se consulta por
`run_id`. `runs` lista exclusivamente ejecuciones manuales como objetos paginados, sin
rollups ni parametro de resolucion. `results` expone exclusivamente series scheduled.
Una ejecucion manual se muestra separadamente y no cambia estado derivado, series
programadas ni alertas.

Al aceptar `run`, el servidor captura la configuracion efectiva del check. Ediciones
posteriores no cambian el destino, timeout ni politica de redirects de ese run o sus
reintentos; la politica SSRF se revalida antes de cada conexion.

Un check desactivado acepta `run` para diagnostico manual. Un check ya archivado
responde `409` con codigo `check_archived`. Si se archiva despues de encolar, el worker
falla el run antes de iniciar red; si ya estaba `running`, puede completar el intento
actual, pero no lo reintenta.

Un `CheckRun` completado tiene esta forma conceptual:

```json
{
  "run_id": "opaque-id",
  "state": "completed",
  "requested_at": "2026-07-15T11:59:59Z",
  "completed_at": "2026-07-15T12:00:00Z",
  "result": {
    "status": "up",
    "latency_ms": 42,
    "error_code": null,
    "http_status": 204
  }
}
```

El `state` de una ejecucion asincrona es `queued`, `running`, `completed` o `failed`.
`result` solo aparece en `completed`; un check down sigue siendo una ejecucion
completada. `failed` se reserva para no poder producir resultado y expone un
`failure_code` seguro (`internal`, `retry_exhausted`, `check_archived`). Un worker usa
lease de `timeout_seconds + 5 segundos` y permite como maximo dos intentos de proceso;
la API conserva un solo `run_id` aunque un crash obligue a repetir la operacion de red.
Las consultas scheduled usan el mismo contrato `raw|hourly|mixed`, manejo del cutoff y
limite de 10000 puntos por serie definido para metricas. Series expuestas:

- `availability`, unidad `ratio`: cada punto raw usa `value=1` para up y `0` para down.
  En hourly, `value=successful_count/sample_count` e incluye ambos contadores;
  `sample_count` cuenta todos los resultados programados completados.
- `latency_ms`, unidad `milliseconds`: solo incluye checks exitosos con latencia. Raw
  representa un resultado y hourly expone promedio en `value`, ademas de `min`, `max`
  y `sample_count` propio de la serie. Como es una serie intencionalmente dispersa,
  omite `expected_sample_count`, `covered_slot_count` y `complete`; la cobertura del
  check se consulta en `availability`.

Fallos o ventanas sin resultados no se convierten en latencia cero.

Configuracion HTTP:

```json
{
  "type": "http",
  "url": "https://service.tailnet.ts.net/health",
  "interval_seconds": 60,
  "timeout_seconds": 10,
  "expected_status_min": 200,
  "expected_status_max": 299,
  "follow_redirects": false,
  "max_redirects": 3
}
```

`max_redirects` acepta valores de 1 a 3; 3 es tope duro de v1 y solo aplica cuando
`follow_redirects` es `true`.

Configuracion TCP:

```json
{
  "type": "tcp",
  "host": "service.tailnet.ts.net",
  "port": 5432,
  "interval_seconds": 60,
  "timeout_seconds": 5
}
```

## 6. Alertas e incidentes

- `GET /api/v1/alert-rules`
- `POST /api/v1/alert-rules`
- `GET /api/v1/alert-rules/{id}`
- `PATCH /api/v1/alert-rules/{id}`
- `DELETE /api/v1/alert-rules/{id}`
- `POST /api/v1/alert-rules/{id}/enable`
- `POST /api/v1/alert-rules/{id}/disable`
- `POST /api/v1/alert-rules/{id}/restore`
- `GET /api/v1/incidents?state=&severity=&cursor=`
- `GET /api/v1/incidents/{id}`

Reglas usan union discriminada por `type`; servidor rechaza campos desconocidos o
incompatibles. Tipos v1:

- `host_unreachable`
- `check_failed`
- `host_metric_threshold`
- `filesystem_threshold`

No existe endpoint para forzar estado de incidente. Estados derivan de evaluacion.
La respuesta de incidente incluye `state` (`open` o `resolved`) y
`condition_status` (`true`, `false` o `unknown`). Un incidente `open` puede tener
condicion `unknown` cuando la evidencia esta stale o ausente; no se resuelve por falta
de datos.

Schemas de condicion:

```json
{"type":"host_unreachable","host_id":"id","for_seconds":180}
{"type":"check_failed","check_id":"id","mode":"duration","for_seconds":180}
{"type":"check_failed","check_id":"id","mode":"attempts","consecutive_failures":3}
{"type":"host_metric_threshold","host_id":"id","metric":"cpu_used_percent","operator":"gte","threshold":90,"for_seconds":300}
{"type":"filesystem_threshold","filesystem_id":"id","operator":"gte","threshold":90,"for_seconds":300}
```

Metricas permitidas en threshold v1: `cpu_used_percent` y
`memory_used_percent`. Operadores: `gt` y `gte`. Threshold va de 0 a 100. Duracion va
de 0 a 86400 segundos; cero permite firing en primera evaluacion valida.

`host_unreachable.for_seconds` se mide directamente desde ultimo pull exitoso, no desde
momento en que estado visual pasa a unreachable. El primer fallo operativo programado inicia pending;
regla dispara cuando edad del ultimo exito alcanza `for_seconds` y sigue existiendo al
menos un fallo operativo. Para host nunca observado se mide desde enrolamiento.

`check_failed` en modo duration mide desde primer resultado programado fallido
consecutivo; modo attempts dispara al alcanzar cantidad indicada. No agrega otra
duracion implicita.

Reglas se evaluan despues de resultados programados, no mediante timer que reutilice
valor viejo. Ejecucion manual de check actualiza solo su resultado manual y no cuenta
para estado derivado, series programadas ni alertas. Datos faltantes, stale o
filesystem desaparecido rompen continuidad y resetean threshold `pending`; incidente
ya `firing` permanece `state=open` con `condition_status=unknown` hasta una muestra
fresh falsa. Host/check unavailable tienen reglas dedicadas. Semantica de archivado,
restauracion y cambios de regla esta en modelo de dominio.

## 7. Canales y entregas

- `GET /api/v1/notification-channels`
- `POST /api/v1/notification-channels`
- `GET /api/v1/notification-channels/{id}`
- `PATCH /api/v1/notification-channels/{id}`
- `DELETE /api/v1/notification-channels/{id}`
- `POST /api/v1/notification-channels/{id}/restore`
- `POST /api/v1/notification-channels/{id}/test`
- `GET /api/v1/notification-deliveries?channel_id=&state=&cursor=`
- `GET /api/v1/notification-deliveries/{id}`

Crear/actualizar Discord acepta `webhook_url` como write-only. El servidor valida que
la URL tenga esquema `https`, hostname exacto `discord.com`, puerto vacio o 443, sin
userinfo, query ni fragment, y path exacto `/api/webhooks/{id}/{token}` con ambos
valores no vacios. Se usa parseo estructurado, no comparacion por prefijo; cualquier
otro destino se rechaza con `422`. Lecturas devuelven:

```json
{
  "id": "opaque-id",
  "name": "Personal Discord",
  "provider": "discord",
  "secret_configured": true,
  "enabled": true
}
```

`test` recibe `{ "request_id": "client-generated-opaque-id" }`, crea una entrega
persistente con `kind=test` y devuelve `202` con
`{ "delivery_id": "id", "status": "queued" }`. Repetir el mismo `request_id` para el
mismo canal devuelve la misma entrega; otro ID crea una prueba nueva. La entrega se
consulta por ID.
Entregas `incident` y `test` pueden terminar `skipped`; la respuesta incluye una razon
estable y no expone configuracion secreta del canal. Antes de cada claim, el worker
revalida que el canal siga habilitado y no archivado; en caso contrario marca
`skipped` sin red.

## 8. Configuracion general

- `GET /api/v1/settings`
- `PATCH /api/v1/settings`
- `GET /api/v1/system/status`

Settings editables v1: retencion, defaults de intervalos, CIDRs privados permitidos
para checks y zona horaria de presentacion. Los checks solo permiten Tailscale y esos
CIDRs; destinos publicos estan bloqueados por default. Base de datos sigue almacenando
UTC.

Rangos:

- Poll/check interval: 15 a 3600 segundos.
- Timeout: 1 a 60 segundos y siempre menor que intervalo.
- Retencion raw: 24 horas a 30 dias.
- Retencion rollup: 7 a 730 dias y no menor que retencion raw despues de convertir
  ambas duraciones a segundos. Por tanto, si raw supera 7 dias, el minimo efectivo de
  rollup pasa a ser el valor de raw.

Intervalos nuevos aplican al primer slot que se cree despues del cambio. Cada slot
conserva intervalo y timeout efectivos; el cambio no modifica slots ni deadlines ya
emitidos. Mientras el periodo siga activo, el siguiente `nominal_at` se calcula desde
el nominal anterior, no desde `received_at`. Tras downtime, un slot fuera de deadline
se cancela y abre un periodo nuevo anclado al presente. Aumentar retencion no
reconstruye datos ya eliminados. Reducir retencion aplica en siguiente job y UI debe
advertir que es irreversible. CIDRs se expresan en notacion CIDR y no
pueden incluir rangos siempre bloqueados. Cada ejecucion resuelve DNS y exige que todas
las IPs resultantes cumplan la politica; redirects HTTP se validan de nuevo.

`system/status` autenticado puede incluir version, estado de jobs, tamano aproximado
de DB y ultimo backup conocido si instalacion lo registra. No incluye secretos.

## 9. Protocolo del agente

### Auth

Servidor envia:

```text
Authorization: Bearer <per-agent-credential>
```

Agente compara hash en tiempo constante y aplica rate limit. Credencial nunca aparece
en query string.

### `GET /agent/v1/healthz`

Puede ser publico dentro de listener del agente y solo devuelve `{"status":"ok"}`.
No sustituye snapshot autenticado.

### `GET /agent/v1/snapshot`

Contrato JSON normativo exacto:

```json
{
  "protocol_version": 1,
  "agent_version": "dev",
  "host": {
    "hostname": "oracle-vm",
    "observed_at": "2026-07-15T12:00:00Z",
    "boot_id": "opaque-linux-boot-id",
    "uptime_seconds": 12345,
    "cpu_used_percent": 12.5,
    "memory_used_bytes": 1024,
    "memory_total_bytes": 4096
  },
  "tailscale": {
    "state": "connected",
    "hostname": "oracle-vm",
    "addresses": ["100.64.0.1"]
  },
  "collection_warnings": [],
  "filesystems": [
    {
      "mount_point": "/",
      "device": "/dev/sda1",
      "fs_uuid": "a1b2c3d4",
      "mount_root": "/",
      "major_minor": "8:1",
      "fs_type": "ext4",
      "used_bytes": 1024,
      "total_bytes": 4096,
      "available_bytes": 3072
    }
  ]
}
```

`protocol_version` es el entero exacto `1`. `agent_version` es string de 1 a 64
caracteres: builds locales permiten `dev` y releases usan semver. `host.hostname` es
string no vacio y `host.observed_at` RFC 3339 UTC. `boot_id` es string no vacio o
null; uptime es entero >=0 o null; CPU es numero 0-100 o null. Memoria usada/total son
enteros >=0 y `used <= total`, o ambos null.

Todos los campos enumerados del objeto raiz, `host`, `tailscale` y cada filesystem son
requeridos. Los campos nullable deben aparecer con un valor valido o `null`; omitirlos
no equivale a enviarlos como `null`. Objetos, arrays y campos no-null no aceptan
`null`, y cualquier campo adicional invalida el payload.

`tailscale` siempre es objeto. `state` es
`connected|stopped|needs_login|unknown`, `hostname` es string no vacio o null y
`addresses` siempre es array de strings, posiblemente vacio. `filesystems` siempre es
array, posiblemente vacio. Cada filesystem contiene exactamente `mount_point`,
`device`, `fs_uuid`, `mount_root`, `major_minor`, `fs_type`, `used_bytes`,
`total_bytes`, `available_bytes`: los campos string salvo `fs_uuid` son no vacios;
`fs_uuid` es string no vacio o null; bytes son enteros >=0, con `used <= total` y
`available <= total`.

`collection_warnings` es un campo requerido, no-null, y siempre es un array de codigos
unicos, posiblemente `[]`. Su omision o `null` invalida el payload.
Codigos exactos iniciales:
`cpu_unavailable`, `memory_unavailable`, `uptime_unavailable`,
`boot_id_unavailable`, `filesystems_partial`, `filesystems_unavailable`,
`tailscale_unavailable`. Un warning no invalida los campos sanos. `clock_skew` nunca
viaja en este wire: el servidor lo agrega a `warnings` de su API/UI cuando la diferencia
absoluta entre `observed_at` y `received_at` supera cinco minutos.

Una lista de filesystems sin warning es autoritativa. `filesystems_partial` indica que
solo deben actualizarse entradas recibidas; `filesystems_unavailable` conserva todo el
estado anterior. Son mutuamente excluyentes; recibir ambos hace el payload invalido de
protocolo, falla el pull y no muta snapshot, filesystems, estado ni scheduling de exito.
El slot se cierra como fallido y se crea el siguiente slot nominal, pero un fallo de
protocolo no modifica ningun campo de `hosts`: estado observado, version, timestamps y
contadores conservan sus valores previos. Tampoco modifica snapshot ni filesystems.
No contribuye a `consecutive_failures` ni al umbral `unreachable`; estos solo incluyen
fallos operativos de red, TLS, autenticacion, timeout o payload excesivo.

Ante fallo de un collector, el campo afectado usa su null/objeto/array seguro y se
agrega el warning correspondiente. En particular, Tailscale no disponible produce
`{"state":"unknown","hostname":null,"addresses":[]}` mas
`tailscale_unavailable`. Un estado desconectado usa `stopped` o `needs_login`, conserva
hostname/addresses parciales que entregue la CLI y degrada el host sin inventar el
warning de indisponibilidad.

El agente es el unico proceso que parsea mountinfo, correlaciona `/dev/disk/by-uuid`,
normaliza UUID/device y produce este wire (`fs_uuid` nullable). El servidor valida los
campos ya normalizados, elige UUID-first o fallback y calcula el codec BLOB v1
documentado en el modelo de dominio. UUID inaccesible/ambiguo ya llega como null sin
warning. `major_minor` es solo diagnostico y no forma parte de la identidad.
Si `device` es absoluto debe ser exactamente `filepath.Clean(device)`; los sources no
absolutos se conservan byte por byte.

CPU se calcula en agente desde dos lecturas de `/proc/stat` separadas por 250 ms:
`100 * (delta_total - delta_idle - delta_iowait) / delta_total`. Memoria usada es
`MemTotal - MemAvailable`. Si contador no avanza o campos no existen, valor JSON es
`null`; servidor conserva el campo actual pero Fase 2A no crea `MetricSample`. Totales
y usados de memoria tambien pueden ser `null` como par.

`observed_at` es diagnostico reportado por agente. Servidor agrega `received_at` al
persistir; orden de series y evaluaciones usan ese timestamp, evitando depender del
reloj del host. Frescura y staleness usan `freshness_deadline_at`, calculado desde el
primer slot creado despues del ultimo exito y nunca avanzado por un fallo.

Payload tiene limite estricto de 256 KiB. Campos nuevos deben ser opcionales dentro de
version o requieren nueva version incompatible. Servidor acepta versiones
explicitamente soportadas; no intenta adivinar formatos.

## 10. Compatibilidad

- Cambios aditivos opcionales mantienen `/v1`.
- Renombrar, quitar o cambiar semantica requiere `/v2` o migracion coordinada.
- UI embebida y servidor se publican juntos, pero API conserva contratos para pruebas
  y futura reutilizacion.
- Agente puede actualizarse separado; servidor debe documentar rango de versiones de
  protocolo soportadas.

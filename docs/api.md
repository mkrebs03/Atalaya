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

Repetir mismo `request_id` y token dentro de ventana devuelve mismo resultado de forma
segura. Ventana dura 15 minutos desde primer consumo, aunque expiracion original ocurra
antes. Despues, replay responde conflicto de token consumido. Implementacion debe
limitar intentos y tamano de request. El replay solo devuelve la misma credencial si
coinciden token, `request_id` y hash del payload completo.

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
momento en que estado visual pasa a unreachable. Primer fallo programado inicia pending;
regla dispara cuando edad del ultimo exito alcanza `for_seconds` y sigue existiendo al
menos un fallo. Para host nunca observado se mide desde enrolamiento.

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

Respuesta conceptual:

```json
{
  "protocol_version": 1,
  "agent_version": "semver",
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

`fs_uuid` es nullable. Agente lo obtiene sin ejecutar comandos privilegiados cuando
puede correlacionar el dispositivo con identificadores expuestos por Linux. Servidor
usa UUID, `mount_root` y mount point como identidad preferida; si falta, aplica el
fallback documentado en el modelo de dominio sin fusionar entidades por heuristica.
`major_minor` es diagnostico y apoya la normalizacion del dispositivo del fallback; no
forma parte de la identidad.

CPU se calcula en agente desde dos lecturas de `/proc/stat` separadas por 250 ms:
`100 * (delta_total - delta_idle - delta_iowait) / delta_total`. Memoria usada es
`MemTotal - MemAvailable`. Si contador no avanza o campos no existen, valor JSON es
`null`; servidor conserva snapshot pero no crea muestra para esa metrica. Totales y
usados de memoria tambien pueden ser `null` como par.

`observed_at` es diagnostico reportado por agente. Servidor agrega `received_at` al
persistir; orden de series y evaluaciones usan ese timestamp, evitando depender del
reloj del host. Frescura y staleness usan `freshness_deadline_at`, calculado desde el
primer slot creado despues del ultimo exito y nunca avanzado por un fallo.

Payload tiene limite estricto. Campos nuevos deben ser opcionales dentro de version o
requieren nueva version incompatible. Servidor acepta versiones explicitamente
soportadas; no intenta adivinar formatos.

## 10. Compatibilidad

- Cambios aditivos opcionales mantienen `/v1`.
- Renombrar, quitar o cambiar semantica requiere `/v2` o migracion coordinada.
- UI embebida y servidor se publican juntos, pero API conserva contratos para pruebas
  y futura reutilizacion.
- Agente puede actualizarse separado; servidor debe documentar rango de versiones de
  protocolo soportadas.

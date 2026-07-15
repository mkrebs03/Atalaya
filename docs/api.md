# API y protocolos

## 1. Convenciones

- API de UI: `/api/v1` sobre mismo origen que frontend.
- API expuesta por agentes: `/agent/v1`.
- JSON UTF-8 y `Content-Type: application/json`.
- Nombres JSON en `snake_case`.
- Timestamps RFC 3339 UTC.
- IDs opacos.
- Mutaciones UI requieren sesion y proteccion CSRF, excepto login pre-auth definido
  abajo.
- Respuestas nunca incluyen hashes, ciphertexts, tokens ni secretos.

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
- `409`: version, unicidad o transicion en conflicto.
- `412`: `If-Match` no coincide con version actual.
- `422`: estructura valida pero configuracion de dominio invalida.
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

### `POST /api/v1/auth/login`

Acepta username y password. Crea cookie de sesion; no devuelve bearer token.
Rate-limited. Error no distingue usuario inexistente de password incorrecto.
Login no exige token CSRF porque todavia no existe sesion. Debe validar `Origin` contra
origen publico configurado y rechazar `Sec-Fetch-Site: cross-site`; navegadores que no
envien Fetch Metadata siguen sujetos a Origin, cookie SameSite y rate limit global.

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
limitar intentos y tamano de request.

## 3. Auth y sesion

- `POST /api/v1/auth/logout`
- `GET /api/v1/auth/me`
- `POST /api/v1/auth/change-password`
- `GET /api/v1/auth/csrf`

Logout y cambio de password requieren CSRF. Cambio de password revoca otras sesiones;
puede emitir sesion nueva para request actual.

Todo recurso mutable devuelve `version` entero y header `ETag: "<version>"`. `PATCH`,
`DELETE`, enable/disable y otras mutaciones sobre recurso existente requieren
`If-Match: "<version>"`. Exito incrementa version. Mismatch responde 412 con version
actual; ausencia responde 428. Acciones asincronas que no editan configuracion, como
`run` y `test`, no requieren `If-Match`.

## 4. Hosts y enrolamiento

- `GET /api/v1/hosts`
- `GET /api/v1/hosts/{id}`
- `PATCH /api/v1/hosts/{id}`
- `DELETE /api/v1/hosts/{id}`
- `POST /api/v1/hosts/{id}/enable`
- `POST /api/v1/hosts/{id}/disable`
- `GET /api/v1/hosts/{id}/filesystems`
- `PATCH /api/v1/hosts/{id}/filesystems/{filesystem_id}`
- `GET /api/v1/hosts/{id}/metrics?from=&to=&resolution=auto`
- `POST /api/v1/enrollment-tokens`
- `GET /api/v1/enrollment-tokens`
- `DELETE /api/v1/enrollment-tokens/{id}`

Crear token devuelve secreto una vez. Listarlo despues solo muestra metadata, estado y
expiracion.

`DELETE /hosts/{id}` realiza baja logica si existe historico. Debe requerir confirmacion
explicita desde UI, pero v1 no necesita reautenticacion.

Respuesta de metricas incluye resolucion real y series alineables sin inventar puntos:

```json
{
  "from": "2026-07-14T00:00:00Z",
  "to": "2026-07-15T00:00:00Z",
  "resolution": "raw",
  "series": [
    {
      "key": "cpu_used_percent",
      "unit": "percent",
      "points": [
        {
          "timestamp": "2026-07-14T00:00:00Z",
          "value": 12.5,
          "min": 8.1,
          "max": 18.4,
          "sample_count": 58,
          "expected_sample_count": 60,
          "complete": false
        }
      ]
    }
  ]
}
```

Para resolucion `raw`, `min` y `max` igualan `value`, `sample_count` vale 1 y
`expected_sample_count` puede omitirse. Para rollup, `value` es promedio y cobertura
permite marcar ventana parcial. Ventana sin muestras no produce punto. UI no une como
continuos puntos separados por ventana ausente y distingue visualmente rollup parcial.
No usar cero o `null` sintetico como medicion.

## 5. Checks

- `GET /api/v1/checks`
- `POST /api/v1/checks`
- `GET /api/v1/checks/{id}`
- `PATCH /api/v1/checks/{id}`
- `DELETE /api/v1/checks/{id}`
- `POST /api/v1/checks/{id}/enable`
- `POST /api/v1/checks/{id}/disable`
- `POST /api/v1/checks/{id}/run`
- `GET /api/v1/checks/{id}/results?from=&to=&resolution=auto`

`run` programa ejecucion y devuelve `202`; no mantiene request HTTP abierto esperando
red destino. Polling posterior obtiene resultado.

Configuracion HTTP:

```json
{
  "type": "http",
  "url": "https://service.tailnet.ts.net/health",
  "timeout_seconds": 10,
  "expected_status_min": 200,
  "expected_status_max": 299,
  "follow_redirects": true
}
```

Configuracion TCP:

```json
{
  "type": "tcp",
  "host": "service.tailnet.ts.net",
  "port": 5432,
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
- `GET /api/v1/incidents?state=&severity=&cursor=`
- `GET /api/v1/incidents/{id}`

Reglas usan union discriminada por `type`; servidor rechaza campos desconocidos o
incompatibles. Tipos v1:

- `host_unreachable`
- `check_failed`
- `host_metric_threshold`
- `filesystem_threshold`

No existe endpoint para forzar estado de incidente. Estados derivan de evaluacion.

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
valor viejo. Ejecucion manual de check actualiza estado/historico visible, pero no
cuenta para alertas. Datos faltantes, stale o filesystem desaparecido rompen continuidad
y resetean threshold `pending`; incidente ya `firing` permanece abierto como `unknown`
hasta muestra fresh falsa. Host/check unavailable tienen reglas dedicadas. Semantica de
edicion y desactivacion esta en modelo de dominio.

## 7. Canales y entregas

- `GET /api/v1/notification-channels`
- `POST /api/v1/notification-channels`
- `GET /api/v1/notification-channels/{id}`
- `PATCH /api/v1/notification-channels/{id}`
- `DELETE /api/v1/notification-channels/{id}`
- `POST /api/v1/notification-channels/{id}/test`
- `GET /api/v1/notification-deliveries?channel_id=&state=&cursor=`

Crear/actualizar Discord acepta `webhook_url` como write-only. Lecturas devuelven:

```json
{
  "id": "opaque-id",
  "name": "Personal Discord",
  "provider": "discord",
  "secret_configured": true,
  "enabled": true
}
```

`test` crea entrega persistente marcada como prueba y devuelve `202`.

## 8. Configuracion general

- `GET /api/v1/settings`
- `PATCH /api/v1/settings`
- `GET /api/v1/system/status`

Settings editables v1: retencion, defaults de intervalos, CIDRs privados permitidos
para checks y zona horaria de presentacion. Base de datos sigue almacenando UTC.

Rangos:

- Poll/check interval: 15 a 3600 segundos.
- Timeout: 1 a 60 segundos y siempre menor que intervalo.
- Retencion raw: 24 horas a 30 dias.
- Retencion rollup: 7 a 730 dias y no menor que retencion raw.

Intervalos nuevos aplican al siguiente schedule. Aumentar retencion no reconstruye
datos ya eliminados. Reducir retencion aplica en siguiente job y UI debe advertir que
es irreversible. CIDRs se expresan en notacion CIDR y no pueden incluir rangos siempre
bloqueados.

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
      "fs_type": "ext4",
      "used_bytes": 1024,
      "total_bytes": 4096,
      "available_bytes": 3072
    }
  ]
}
```

CPU se calcula en agente desde dos lecturas de `/proc/stat` separadas por 250 ms:
`100 * (delta_total - delta_idle - delta_iowait) / delta_total`. Memoria usada es
`MemTotal - MemAvailable`. Si contador no avanza o campos no existen, valor JSON es
`null`; servidor conserva snapshot pero no crea muestra para esa metrica. Totales y
usados de memoria tambien pueden ser `null` como par.

`observed_at` es diagnostico reportado por agente. Servidor agrega `received_at` al
persistir; frescura, staleness, orden de series y alertas usan exclusivamente
`received_at`, evitando depender del reloj del host.

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

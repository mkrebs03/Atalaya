# Modelo de dominio y datos

Este documento define semantica. Nombres exactos de tablas pueden variar, pero
invariantes y relaciones deben conservarse.

## 1. Convenciones

- IDs publicos son UUID aleatorios opacos; nunca IDs SQLite incrementales.
- Timestamps se almacenan en UTC y se exponen como RFC 3339 con precision suficiente.
- Duraciones API usan segundos enteros salvo latencia, expresada en milisegundos.
- Porcentajes API usan numeros entre `0` y `100`.
- Bytes usan enteros, nunca strings formateados.
- Entidades configurables incluyen `created_at`, `updated_at`, `version` entero
  monotono y `enabled` cuando corresponda.
- Entidades configurables incluyen `archived_at` nullable. Archivar es baja logica; no
  se borran fisicamente desde UI.
- Eliminacion de configuracion puede ser logica cuando exista historico referenciado.
- Escrituras concurrentes usan `version`; contrato HTTP esta definido en `api.md`.

## 2. Entidades

### Administrator

Unico usuario v1. Campos conceptuales:

- `id`, `username`, `password_hash`.
- `created_at`, `updated_at`, `password_changed_at`.
- `disabled_at`, reservado para bloqueo administrativo local.

Base de datos impide mas de un administrador activo.

### Session

- UUID, FK al administrador y hash SHA-256 unico del token como BLOB de 32 bytes;
  nunca token en claro.
- `created_at`, `last_activity_at`, `idle_expires_at`, `absolute_expires_at` y
  `revoked_at` nullable, todos RFC3339 UTC. Idle dura 24 horas y absoluta 7 dias;
  alcanzar cualquiera invalida sin reactivacion.
- `remote_addr` observado por el servidor y user-agent UTF-8 truncado a 512 bytes. Bajo Tailscale Serve,
  `remote_addr` normalmente es loopback y no se presenta como IP del usuario.
- Identidad reenviada por el proxy local, nullable y marcada `informational`; nunca se
  usa para autenticar, autorizar o atribuir una IP tailnet.

Cambiar o resetear password revoca todas las sesiones existentes; el cambio
autenticado puede emitir una sesion nueva para el request actual.

### Host

- Nombre visible unico.
- Nombre MagicDNS o URL de agente unica.
- Estado administrativo: `pending`, `active`, `disabled`.
- Archivado: `archived_at` nullable. Un host archivado no se agenda y solo puede
  recuperarse mediante restauracion administrativa.
- Estado observado derivado: `unknown`, `healthy`, `degraded`, `unreachable`.
- Intervalo, timeout, ultimo intento, ultimo exito y fallos consecutivos.
- `freshness_deadline_at`, capturado solo al persistir un exito como deadline del primer
  slot siguiente mas 5 segundos. Fallos y reanclajes no lo avanzan. Un cambio de
  configuracion no reescribe ese valor.
- Credencial de agente cifrada y version de protocolo observada.

`unreachable` requiere tres intentos programados fallidos consecutivos y alcanzar el
deadline del tercer slot; cada slot conserva su intervalo efectivo. No cuentan
ejecuciones manuales, schedules omitidos ni interrupciones por shutdown. No prueba si
causa es host, agente, Tailscale o red.

### EnrollmentToken

- Hash del token, etiqueta opcional, expiracion y creador.
- Un unico uso exitoso y fecha de revocacion.
- `request_id`, hash del payload, `consumed_at`, `replay_until` y host enrolado para
  idempotencia. El replay exacto recupera la credencial cifrada del host durante esa
  ventana, sin crear otro host ni otra credencial.

Token en claro se muestra una vez y nunca se persiste.

### HostSnapshot

Estado actual normalizado:

- Timestamp observado por servidor y timestamp reportado por agente.
- CPU usada, nullable si plataforma no pudo calcular intervalo valido.
- Memoria usada y total, nullable si `/proc/meminfo` no ofrece datos validos.
- Uptime y boot ID cuando Linux lo permita.
- Estado Tailscale reportado, IPs y hostname no sensibles.
- Version del agente.

Timestamp del servidor ordena series. Timestamp del agente sirve para diagnosticar
clock skew. Servidor rechaza o marca snapshots con desviacion excesiva.

### Filesystem

Identidad dentro de host con procedencia explicita:

- `identity_kind=uuid|fallback` e `identity_key` inmutable.
- UUID de filesystem nullable, root del mount, major:minor, mount point, tipo,
  dispositivo, total, usado y disponible.
- Descubierto, seleccionado para monitoreo o ignorado.
- Ultima vez observado.

Cuando existe UUID, la clave es `uuid + mount_root + mount_point`; asi un cambio de
`/dev/sdX` no rompe la serie y dos mounts permanecen distinguibles. Sin UUID, la clave
fallback usa dispositivo normalizado, mount root y mount point. En ambos casos, mover
el mount crea una entidad nueva. El fallback es best-effort: si cambia cualquiera de
sus componentes nunca se fusiona historico por heuristica.

Pseudo-filesystems como `proc`, `sysfs`, `tmpfs`, `devtmpfs`, `cgroup`, `overlay` y
similares se excluyen por default. Exclusion exacta es lista versionada y testeada.

### ServiceCheck

- Nombre, tipo `http` o `tcp`, destino y host relacionado opcional.
- Intervalo, timeout y estado administrativo.
- Archivado: `archived_at` nullable. Un check archivado no se agenda y conserva sus
  resultados.
- Configuracion tipada por clase de check.
- Estado actual derivado: `unknown`, `up`, `down`.
- Ultimo intento, ultimo exito, latencia y error normalizado.
- `freshness_deadline_at` con la misma semantica que host. Cualquier resultado
  programado completado (`up` o `down`) lo actualiza; un check caido entrega datos
  frescos. Solo un slot `failed` por imposibilidad interna no lo actualiza.

Relacion con host es organizativa; check siempre se ejecuta desde servidor.

### CheckRun

Ejecucion manual asincrona separada del scheduling:

- `id`, check, `request_id` de cliente unico por check para idempotencia de creacion,
  estado `queued|running|completed|failed`, solicitud, inicio, fin, `lease_until` y
  contador de intentos de proceso.
- Snapshot inmutable de tipo, destino, timeout, rango esperado y politica de redirects
  vigente al crear el run; todos los intentos usan ese mismo snapshot.
- Resultado normalizado solo en `completed`: up/down, latencia, categoria de error y
  status HTTP. `failed` usa un codigo operativo seguro (`internal`,
  `retry_exhausted`, `check_archived`) y no representa check down.
- Nunca referencia `ScheduleSlot`, alimenta rollups, cambia estado actual ni evalua
  alertas.

Un worker reclama `queued` en una transaccion, cambia a `running` y toma un lease de
`timeout efectivo + 5 segundos`. Un lease vencido vuelve a `queued` si queda el segundo
y ultimo intento; despues termina `failed` con `retry_exhausted`. El claim vuelve a
leer el check: desactivado permite diagnostico manual, mientras archivado termina
`failed` con `check_archived`. Si se archiva durante la red, el request en curso puede
persistir su resultado, pero no se reintenta. Un crash despues de la red y antes del
commit puede repetir una consulta HTTP GET o conexion TCP; la identidad de `CheckRun`
es unica, pero v1 no promete exactly-once en la red.

### SchedulePeriod

Historia de scheduling compartida por hosts y checks:

- Tipo e ID del target, `starts_at`, `ends_at` nullable, intervalo y timeout efectivos.
  El rango es semiabierto y sus slots siguen
  `nominal_at = starts_at + n * effective_interval`.
- Habilitar un target abre un periodo; cambiar intervalo/timeout cierra el vigente y
  abre otro en la frontera del siguiente schedule; desactivar o archivar lo cierra.
- Los slots nominales derivados del periodo permiten calcular deadlines y muestras
  esperadas sin reinterpretar el pasado con configuracion actual, usando las mismas
  formulas de `not_before_at` y `deadline_at` definidas en `ScheduleSlot`.
- Si un slot emitido vence durante downtime, el periodo termina en
  `min(now, expired.nominal_at + effective_interval)`: conserva ese slot como esperado,
  evita solapamiento y excluye todos los nominales posteriores. Un periodo nuevo
  comienza en `now` con la configuracion vigente; el intervalo entre ambos periodos no
  genera expected count.

### ScheduleSlot

Unidad persistida de ejecucion para host o check:

- `id`, target, `schedule_period_id`, `nominal_at`, `not_before_at`, `deadline_at`,
  intervalo y timeout efectivos.
- `not_before_at` es `nominal_at + jitter elegido al crear el slot`, con jitter maximo
  de 5 segundos. `deadline_at` es
  `nominal_at + jitter maximo (5 segundos) + timeout efectivo`.
- Estado `scheduled`, `running`, `succeeded`, `failed` o `cancelled`; `running` guarda
  un lease de `timeout efectivo + 5 segundos de margen` y contador de intentos de
  proceso. `cancelled` exige razon `missed_deadline`, `target_disabled` o
  `target_archived`.
- Para un host, `succeeded` exige un pull que entrego snapshot valido; cualquier otro
  intento completado es `failed`. Para un check, `succeeded` es una ejecucion que
  produjo resultado `up` o `down`; `failed` se reserva para imposibilidad interna del
  servidor y no representa check down.
- Restriccion unica permite como maximo un slot no terminal por target.
- Resultado scheduled, finalizacion y siguiente slot se escriben en la misma
  transaccion; cada resultado o conjunto de muestras referencia `slot_id` con unicidad
  para evitar duplicados o targets habilitados sin agenda.

En operacion normal, el siguiente slot conserva la cadencia con
`nominal_at = previous.nominal_at + previous.effective_interval`. En restart, un lease
vencido vuelve a `scheduled` y solo se reintenta mientras `now <= deadline_at`. Despues se
cancela como `missed_deadline`, se cierra el periodo y se crea en la misma transaccion
un periodo y slot nuevos anclados en `now`, sin catch-up. El slot vencido permanece en
la cobertura esperada; nominales posteriores del downtime no se materializan, no son
esperados y no cuentan como fallos. Shutdown libera o deja vencer el lease, sin crear
un fallo programado.
Desactivar o archivar cancela el slot no iniciado; una ejecucion de red ya reclamada
puede finalizar, pero la transaccion la marca `cancelled`, no incorpora su resultado a
estado/series/alertas y no crea otro slot.

### MetricSample

Series crudas separan dimensiones para consultas acotadas:

- Metricas de host: CPU y memoria usada/total. Uptime y boot ID pertenecen al estado
  actual del snapshot; no generan series historicas.
- Metricas de filesystem: usado/total por filesystem.
- Resultados scheduled de check: exito, latencia, categoria de error y status HTTP
  opcional; alimentan estado, rollups y alertas.
- Muestras y resultados scheduled referencian `slot_id`; ejecuciones manuales viven en
  `CheckRun` y nunca en esta entidad.

No guardar blobs JSON completos como unica fuente historica. Snapshot original puede
registrarse temporalmente en debug, nunca requerido para consultas.

### HourlyRollup

Por hora UTC y serie:

- `min`, `max`, `avg`, `count` para valores numericos.
- Disponibilidad como exitos/total para checks.
- Valores y `count` se asignan a la ventana de `received_at`. Cobertura guarda por
  separado `expected_sample_count`, derivado de slots nominales de `SchedulePeriod`, y
  `covered_slot_count`, satisfecho por muestras/resultados que referencian esos
  `slot_id`. Periodos desactivados o archivados no generan muestras esperadas; cada
  segmento usa su propio intervalo.
- `complete` exige hora cerrada y todos los slots esperados cubiertos; no compara
  `count` con expected. Una muestra recibida cruzando la hora satisface cobertura de su
  slot nominal, pero su valor se agrega en la hora de `received_at`.
- Series intencionalmente dispersas, como latencia de checks que solo existe en exitos,
  omiten cobertura; availability conserva la cobertura del check.
- Inicio y fin de ventana.

Claves unicas impiden duplicados. Recalcular una ventana produce mismo resultado.
Si falta historia suficiente para reconstruir algun segmento, `expected_sample_count`
y `covered_slot_count` se omiten y `complete=false`; no se inventa cobertura.

### AlertRule

- Nombre, tipo, target, severidad y estado administrativo.
- Archivado: `archived_at` nullable. Archivar resuelve el incidente abierto con razon
  `rule_archived` y detiene evaluaciones.
- Umbral, operador, duracion sostenida y parametros propios del tipo.
- Lista de canales destino.

Configuracion invalida no puede activarse. Archivar o desactivar el target detiene la
evaluacion y resuelve el incidente abierto (`target_archived`/`target_disabled`); la
regla y sus incidentes se conservan. Restaurar y habilitar el target reanuda la
evaluacion desde `inactive`. No se puede agregar un canal archivado a una regla.

### AlertEvaluation

Estado corto por combinacion regla-target:

- Estado `inactive`, `pending` o `firing`.
- Primer instante verdadero, ultima evaluacion y valor observado.
- Resultado de ultima evaluacion: `true`, `false` o `unknown`.
- Referencia a incidente abierto cuando esta firing.

`resolved` no es estado estable de evaluacion; es cierre de incidente. Datos faltantes,
stale o filesystem desaparecido rompen continuidad: resetean `pending` a `inactive`,
pero no resuelven incidente `firing`. Firing queda abierto con evaluacion `unknown`
hasta recibir muestra fresh falsa que pruebe recuperacion. Host/check tienen reglas
propias para ausencia.

### Incident

- Regla y target capturados como referencia y snapshot de nombre.
- Severidad.
- Estado `open` o `resolved`.
- `condition_status`: `true`, `false` o `unknown`. Un incidente puede estar `open` y
  `unknown` cuando falta evidencia fresh; eso no lo resuelve.
- `started_at`, `fired_at`, `resolved_at`.
- Valor inicial, ultimo valor y razon de resolucion.

Solo puede existir un incidente abierto por regla-target. Incidentes son inmutables en
identidad y no se eliminan al archivar configuracion.

### NotificationChannel

- Nombre, proveedor, configuracion publica y secreto cifrado.
- Archivado: `archived_at` nullable. Un canal archivado no recibe entregas nuevas,
  pero conserva entregas historicas.
- Estado administrativo y ultimo test.
- La API devuelve `secret_configured`, nunca ciphertext ni secreto.

### NotificationDelivery

Outbox persistente:

- `kind` (`incident` o `test`), canal y payload final ya renderizado, inmutable y sin
  secreto. Reintentos usan exactamente el mismo cuerpo y metadata no secreta.
- Para `incident`, incidente y transicion `firing|resolved` obligatorios. Para `test`,
  ambos son null y `test_request_id` es obligatorio.
- Estado `pending`, `sending`, `delivered`, `failed`, `skipped`.
- Intentos, proximo intento, ultimo error, razon estable de `skipped` cuando aplica y
  timestamps.

Indice unico parcial por incidente, transicion y canal garantiza deduplicacion de
entregas `incident`. Otro indice por canal y `test_request_id` hace idempotente el
replay exacto de `test`; cada request ID distinto representa una prueba nueva. Tests
no participan en orden firing/resolved.

Cuando un incidente entra en `firing`, captura como destinos todos los canales
configurados en la regla. Se crea una entrega de apertura por destino: `pending` si el
canal es elegible o `skipped` con razon estable si esta desactivado o archivado. Canales
agregados despues no se incorporan a ese incidente.

La resolucion crea entregas para exactamente el mismo snapshot de destinos. Si la
apertura fue `skipped`, o el canal ya no es elegible, la resolucion tambien queda
`skipped`; de otro modo puede enviarse. `skipped` es terminal y queda persistido para
auditoria, sin intento de red.

Worker toma entrega con lease de 60 segundos. `sending` con lease vencido vuelve a
`pending`. En la misma transaccion de claim vuelve a leer el canal: si esta desactivado
o archivado, convierte la entrega a `skipped` con razon estable y no inicia red. Si el
canal cambia despues de pasar a `sending`, el request en curso puede terminar; el
cambio detiene reintentos y entregas aun no reclamadas. Se realizan hasta seis intentos
con backoff acotado y jitter: 5 segundos,
30 segundos, 2 minutos, 10 minutos y 30 minutos entre intentos; `Retry-After` valido
puede extender espera hasta 2 horas. HTTP 429 y 5xx son reintentables; errores 4xx
permanentes terminan `failed`. Por incidente/canal, entrega `resolved` espera que
`firing` termine `delivered`, `failed` o `skipped`, preservando orden sin quedar
bloqueada por una entrega inexistente.

Cada intento Discord usa timeout HTTP total de 15 segundos, no sigue redirects y
revalida la URL exacta antes de abrir conexion. El timeout queda por debajo del lease
para que dos workers no envien concurrentemente. Un crash despues de que Discord
acepte el request pero antes del commit puede producir un reenvio: la entrega logica
sigue deduplicada, pero v1 no promete exactly-once frente al proveedor.

## 3. Transiciones

### Host

```text
pending --first successful pull--> active/healthy
pending --failure threshold------> pending/unreachable
active  --isolated failure-------> active/degraded
active  --failure threshold------> active/unreachable
active  --successful pull--------> active/healthy
*       --admin disables---------> disabled/unknown
disabled --enable----------------> pending|active/unknown
*       --archive-----------------> archived/unknown
archived --restore---------------> disabled/unknown
```

Un pull exitoso despues de `unreachable` recupera host inmediatamente; reglas deciden
si incidente resuelve con una o mas muestras exitosas.

Para un host `pending` nunca observado, la edad se mide desde el enrolamiento y su
estado observado es `unknown` hasta alcanzar el umbral de `unreachable`. `enable`
devuelve el host a `pending` si nunca tuvo un pull exitoso y a `active` en caso
contrario; el estado observado se recalcula con el siguiente pull.

### Check

```text
unknown --success--> up
unknown --failure--> down
up      --failure--> down
down    --success--> up
*       --disabled-> unknown
*       --archive--> archived/unknown
archived --restore-> disabled/unknown
```

Estado actual refleja el ultimo resultado programado. Resultados manuales permanecen
separados. Alertas sostenidas evitan notificaciones por fallos breves.

### Alertas

```text
inactive --condition true----------------> pending
pending  --condition false---------------> inactive
pending  --required duration reached-----> firing + open incident
firing   --condition remains true--------> firing
firing   --condition false---------------> inactive + resolve incident
```

Entrar en firing captura destinos y crea las entregas de apertura; resolver usa ese
mismo snapshot y crea entregas enviables o `skipped` segun su elegibilidad. Cambios
materiales de target, tipo, operador, umbral o duracion resuelven incidente con
`rule_changed` y reinician evaluacion. Cambios de nombre o canales no lo resuelven.
Desactivar regla o target resuelve con `rule_disabled` o `target_disabled`; archivar
resuelve con `rule_archived` o `target_archived`. Datos stale o ausentes mantienen un
incidente firing abierto con `condition_status=unknown` hasta una muestra fresh falsa.

## 4. Retencion y agregacion

Defaults:

- Muestras crudas: 48 horas.
- Rollups horarios: 90 dias.
- Incidentes, configuracion y auditoria esencial: sin expiracion automatica v1.
- Entregas de notificacion: 90 dias por default, preservando resumen en incidente.
- Runs manuales (`CheckRun`): 90 dias por default.
- Slots de schedule terminales: misma retencion que las muestras crudas. Se conservan
  siempre los slots fallidos activos y el ultimo slot exitoso por target.
- Periodos de schedule: sin expiracion automatica en v1. Su volumen es acotado por
  cambios administrativos y reanclajes, y conservarlos evita romper cobertura o claves
  foraneas de slots retenidos.
- Sesiones expiradas y tokens consumidos: limpieza periodica tras 30 dias.

Orden seguro del job:

1. Determinar horas UTC completas anteriores a ventana abierta actual.
2. Insertar o reemplazar rollups de forma idempotente.
3. Confirmar transaccion.
4. Eliminar crudos anteriores a retencion solo si ventanas correspondientes existen.
5. Eliminar datos auxiliares expirados en lotes acotados.

La fuente se elige respecto del cutoff raw efectivo, no por una duracion fija de la
consulta. El tramo anterior al cutoff usa rollups y el tramo reciente disponible usa
crudos, incluso en una consulta de 24 o 48 horas. Las ventanas son semiabiertas y cada
bucket se obtiene de una sola fuente para evitar doble conteo en frontera. El limite
maximo de respuesta es 10000 puntos por serie despues de combinar fuentes; consultas
que lo superen responden 422 sin truncar.

## 5. SQLite

- Activar `PRAGMA foreign_keys=ON` en cada conexion.
- Usar WAL y `busy_timeout`.
- Un writer logico por transaccion corta; red nunca dentro de transaccion.
- Migraciones versionadas. El servidor aplica migraciones pendientes al arrancar,
  antes de aceptar trafico. `atalaya migrate` ejecuta las mismas migraciones sin
  arrancar el servidor, para restauracion y upgrades supervisados; ambos caminos
  comparten implementacion.
- Indices comienzan por identidad de serie y timestamp.
- Limitar tamano de strings, errores y payloads antes de persistir.

DDL exacto pertenece a migraciones. Toda migracion nueva incluye prueba desde base
vacia y desde version anterior soportada.

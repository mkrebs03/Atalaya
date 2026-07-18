# Diseno de Fase 2A: agente local y estado actual

Estado: aprobado para implementacion.

Este documento refina el primer slice de la Fase 2. Esta subordinado a `spec.md` y a
los documentos canonicos enlazados desde `docs/README.md`.

## 1. Objetivo y alcance

Fase 2A hace visible Oracle con metricas Linux reales mediante el mismo protocolo pull
que usaran agentes remotos. Incluye agente local, bootstrap idempotente, scheduling
persistente, snapshot actual, filesystems y resumen/detalle read-only.

Quedan fuera de este slice: tokens y enrolamiento remoto, CRUD de hosts, seleccion de
filesystems desde UI, historico y graficos, alertas, instalador, unidades `systemd` y
configuracion automatica de Tailscale Serve. Fase 2B completa enrolamiento y gestion;
Fase 3 agrega series.

## 2. CLI, configuracion y bootstrap

Comandos nuevos:

```text
atalaya agent --config <path>
atalaya bootstrap local-agent --server-config <path> --agent-config <path> \
  --agent-url <url> [--display-name <name>] [--listen <addr>] \
  [--credential-path <path>]
```

El YAML del agente es estricto y contiene `listen`, `server_url`, `advertised_url` y
`credential_path`. Paths relativos se resuelven respecto al directorio del YAML. Los
defaults del bootstrap son nombre `Oracle`, listener `127.0.0.1:9474` y credencial
junto a la config cuando no se indica path. `server_url` deriva de `public_url` del
server config; `advertised_url` es exactamente `--agent-url` despues de validarlo.

Bootstrap usa dos transacciones separadas por escrituras atomicas:

1. Crea o recupera el unico host `source=local` y su credencial cifrada, dejando
   `bootstrap_completed_at=NULL`.
2. Escribe primero credencial `0600` y despues config `0640` mediante temporales en el
   mismo directorio, `fsync` y rename sin sobrescribir archivos existentes.
3. Completa `bootstrap_completed_at` y crea periodo/primer slot en una segunda
   transaccion.

Hosts incompletos no son visibles ni schedulables. Un rerun descifra el mismo secreto,
repone archivos ausentes y exige coincidencia semantica y permisos exactos de los
existentes. No rota secreto, duplica host, sobrescribe archivos ni reactiva un host
previamente completado. Temporales se limpian al fallar. Cada archivo valido existente
solo se verifica; cada faltante se crea. Si falla la segunda escritura, la credencial
valida creada primero y la identidad/secreto en DB permanecen, y el rerun crea la
config faltante. Una restriccion garantiza un unico host local.

La credencial canonica escrita son exactamente 43 caracteres base64url sin padding
mas LF. Loader/comparacion acepta esos 43 caracteres con cero o un LF final, rechaza
CR, otro whitespace, padding o bytes extra, decodifica exactamente 32 bytes y compara
en tiempo constante. Un archivo existente valido sin LF no se reescribe.

Config existente se parsea como YAML estricto `KnownFields` de un unico documento.
`credential_path` relativo se resuelve contra el directorio del YAML. La igualdad
semantica compara: `listen` textual exacto despues de validacion host:port,
`server_url` y `advertised_url` en su forma canonica parseada, y credential path
absoluto lexicalmente limpio. Orden y comentarios no importan; modo debe ser exactamente
`0640`. Config semanticamente igual no se reescribe y cualquier mismatch falla.

## 3. Agente y snapshot

El agente recolecta sin root:

- CPU con dos lecturas de `/proc/stat` separadas por 250 ms.
- Memoria desde `/proc/meminfo`, uptime, boot ID y hostname.
- Mounts desde `/proc/self/mountinfo`, capacidad mediante `statfs` y UUID mediante
  `/dev/disk/by-uuid`.
- Estado propio de Tailscale mediante `tailscale status --json`, con timeout, salida
  limitada y sin registrar JSON crudo.

La lista cerrada v1 de `fs_type` excluidos es `autofs`, `binfmt_misc`, `bpf`, `cgroup`,
`cgroup2`, `configfs`, `debugfs`, `devpts`, `devtmpfs`, `efivarfs`, `fusectl`,
`hugetlbfs`, `mqueue`, `nsfs`, `overlay`, `proc`, `pstore`, `ramfs`, `rootfs`,
`rpc_pipefs`, `securityfs`, `selinuxfs`, `squashfs`, `sysfs`, `tmpfs`, `tracefs`.
Ningun otro tipo se excluye por nombre. Filesystems elegibles nuevos nacen
seleccionados.

Mountinfo decodifica solo `\040`, `\011`, `\012`, `\134`; escapes distintos o
malformed rechazan la entrada. Mount root/point deben ser absolutos y usan
`filepath.Clean` lexical preservando case. Device/source que comienza `/` recibe clean
lexical sin `EvalSymlinks`; otro source conserva bytes decodificados. UUID se toma del
nombre `/dev/disk/by-uuid` correlacionado por `stat` major:minor y se normaliza ASCII
lowercase. Inaccesible o ambiguo cae a fallback sin warning. `identity_key` es BLOB v1:
byte `0x01`, luego cada uno de tres campos como longitud en bytes uint32 big-endian y
bytes UTF-8. Kind se guarda aparte. UUID ordena
`(lowercase_uuid,mount_root,mount_point)`; fallback ordena
`(normalized_device,mount_root,mount_point)`. La unicidad es
`(host_id,identity_kind,identity_key)`. Campo mayor a uint32 o UTF-8 invalido se
rechaza. Major:minor no participa; no hay alias/fusion y cambiar componente crea
entidad. Golden tests fijan ambos BLOB y rechazos.

El agente expone `GET /agent/v1/healthz` y `GET /agent/v1/snapshot`. Snapshot exige
bearer de 32 bytes base64url guardado en archivo regular `0600`; su hash se compara en
tiempo constante. Se limita a 60 requests/minuto con burst 10 y una sola recoleccion
simultanea.

`collection_warnings` aparece siempre en protocolo v1, aunque sea `[]`. Codigos
iniciales: `cpu_unavailable`, `memory_unavailable`, `uptime_unavailable`,
`boot_id_unavailable`, `filesystems_partial`, `filesystems_unavailable`,
`tailscale_unavailable`. Un fallo parcial no invalida campos sanos ni expone detalles
sensibles. El agente nunca emite `clock_skew`: el servidor lo agrega a `warnings` de
API/UI cuando `abs(observed_at-received_at) > 5 minutos`.

El wire exacto tiene `protocol_version: 1`, `agent_version` string 1..64 (`dev` o
semver de release), y `host` con hostname string, observed_at RFC3339, boot_id
string|null, uptime entero >=0|null, CPU 0..100|null y memoria usada/total ambas
enteros >=0 o ambas null. `tailscale` siempre es objeto con state
`connected|stopped|needs_login|unknown`, hostname string|null y addresses array de
strings. `filesystems` siempre es array; cada item contiene mount_point, device,
mount_root, major_minor y fs_type strings, fs_uuid string|null, y used/total/available
bytes enteros >=0. `collection_warnings` es requerido y no-null; puede ser `[]`.

Un collector fallido usa null para su metrica y warning correspondiente. Tailscale
indisponible usa state unknown, hostname null, addresses `[]` y warning; stopped o
needs_login conserva datos parciales de CLI y degrada. Filesystems parcial conserva
solo items validos con warning; unavailable envia `[]` con warning.

Una lista de filesystems sin warning es autoritativa y marca ausentes como
`present=false`. Con `filesystems_partial` solo se actualizan entradas recibidas; con
`filesystems_unavailable` se conserva todo el estado anterior. Un snapshot valido,
incluso parcial, reinicia fallos y avanza frescura. Warnings, Tailscale no conectado o
desviacion absoluta de reloj mayor a cinco minutos producen estado `degraded`.
Los dos warnings de filesystem son mutuamente excluyentes; ambos juntos invalidan el
protocolo y el pull falla sin mutacion.
El scheduler puede cerrar el slot como fallido y crear el siguiente slot nominal, pero
no cambia ningun campo del host, snapshot o filesystems ante un fallo de protocolo.
Tampoco incrementa `consecutive_failures` ni contribuye al umbral `unreachable`; solo
los fallos operativos programados lo hacen.

## 4. Scheduler, persistencia y red

La migracion del slice agrega hosts, snapshot actual, warnings/direcciones actuales,
filesystems, periodos y slots. IDs de slot son unicos, timestamps se guardan en UTC y
existe como maximo un slot no terminal por target. Fase 2A no crea `MetricSample` ni
historico raw; esa persistencia comienza en Fase 3.

Defaults: intervalo 60 segundos, timeout 10 segundos, jitter uniforme 0-5 segundos,
lease de timeout mas 5 segundos, concurrencia global 4 y payload maximo 256 KiB.
Scheduler reclama antes de red y finaliza resultado, estado y siguiente slot en una
transaccion. Nunca mantiene una transaccion durante HTTP.

Tras restart recupera leases, cancela vencidos como `missed_deadline` y reancla sin
catch-up. Si target cambia durante red, descarta resultado. El tercer fallo agenda
`unreachable` para el deadline persistido de ese slot; hasta entonces queda
`unknown` si nunca fue observado. Un host previamente observado queda `degraded`
antes del threshold. Un exito recupera inmediatamente.

`agent_url` acepta exclusivamente `https://<fqdn>.ts.net:9474`, sin userinfo, path,
query ni fragment. Cada dial resuelve otra vez y exige que todas las IP pertenezcan a
`100.64.0.0/10` o `fd7a:115c:a1e0::/48`. Conecta una IP validada conservando hostname
para SNI, usa trust store del sistema, ignora proxies de entorno y no sigue redirects.

Los cambios relevantes para pull aumentan un `collection_generation` interno. Si la
generacion cambia durante red, el slot termina `target_changed`, se cierra su periodo
y se reancla con la configuracion vigente; `Host.version` no se usa como CAS porque
tambien cambia por snapshots y transiciones visibles.

## 5. API y frontend

Este slice implementa solamente:

- `GET /api/v1/hosts`
- `GET /api/v1/hosts/{id}`
- `GET /api/v1/hosts/{id}/filesystems`

Todos exigen sesion. Listas aceptan `limit` default 50/maximo 100 y cursor opaco, y
usan `items`/`next_cursor`. Errores: 400 `validation_failed` con fields para
limit/cursor, 401 `authentication_required`, 404 `not_found` para host
ausente/oculto/incompleto. Hosts ordenan por display name case-insensitive ASC,id ASC
y el cursor codifica esa ultima tupla normalizada; filesystems ordenan mount point
ASC,id ASC. Sin snapshot aislado, cambios concurrentes pueden mover items y cliente
puede reiniciar paginacion. Detalle devuelve strong `ETag: "<version>"`, no-store y
304 ante `If-None-Match` coincidente.

Shapes normativos (los campos mostrados son exactos):

```json
{"items":[{"id":"id","display_name":"Oracle","administrative_state":"pending","observed_state":"unknown","stale":false,"last_attempt_at":null,"last_success_received_at":null,"freshness_deadline_at":null,"consecutive_failures":0,"cpu_used_percent":null,"memory_used_bytes":null,"memory_total_bytes":null,"uptime_seconds":null,"tailscale":null,"warnings":[],"agent_version":null,"protocol_version":null,"most_constrained_filesystem":null,"version":1}],"next_cursor":null}
```

```json
{"id":"id","display_name":"Oracle","administrative_state":"pending","observed_state":"unknown","stale":false,"last_attempt_at":null,"last_success_received_at":null,"freshness_deadline_at":null,"consecutive_failures":0,"cpu_used_percent":null,"memory_used_bytes":null,"memory_total_bytes":null,"uptime_seconds":null,"tailscale":null,"warnings":[],"agent_version":null,"protocol_version":null,"most_constrained_filesystem":null,"agent_url":"https://oracle.tailnet.ts.net:9474","snapshot":null,"version":1}
```

Cuando existen, los objetos anidados exactos sustituyen sus null:

```json
{"tailscale":{"state":"connected","hostname":"oracle.tailnet.ts.net","addresses":["100.64.0.1"]},"most_constrained_filesystem":{"id":"fs-id","mount_point":"/","used_percent":25.0,"used_bytes":1024,"total_bytes":4096},"snapshot":{"received_at":"2026-07-17T12:00:00Z","observed_at":"2026-07-17T12:00:00Z","hostname":"oracle","boot_id":"boot-id","uptime_seconds":123,"cpu_used_percent":12.5,"memory_used_bytes":1024,"memory_total_bytes":4096,"tailscale":{"state":"connected","hostname":"oracle.tailnet.ts.net","addresses":["100.64.0.1"]},"collection_warnings":[]}}
```

```json
{"items":[{"id":"fs-id","identity_kind":"uuid","mount_point":"/","device":"/dev/sda1","mount_root":"/","major_minor":"8:1","fs_type":"ext4","selected":true,"present":true,"used_bytes":1024,"total_bytes":4096,"available_bytes":3072,"last_observed_at":"2026-07-17T12:00:00Z","version":1}],"next_cursor":null}
```

El summary usa timestamps RFC3339 UTC nullable; CPU es porcentaje 0-100 nullable,
memoria/filesystem son bytes enteros no negativos nullable y uptime segundos nullable.
`tailscale`, versiones y filesystem mas comprometido son nullable. Este ultimo contiene
`id`, `mount_point`, `used_percent`, `used_bytes`, `total_bytes` y solo considera
selected+present con total valido. Detalle agrega `agent_url` y snapshot completo con
`received_at`, `observed_at`, `hostname`, boot ID/metricas nullables, Tailscale siempre
objeto y `collection_warnings` siempre presente. Filesystem usa
`identity_kind=uuid|fallback`;
sus strings son no vacios, bytes/ultima observacion son nullable y todo item incluye
`version`. `warnings` de API combina codigos del agente y derivados del servidor.
`last_attempt_at` solo es null antes del primer intento; success/freshness solo antes
del primer snapshot valido. El host local empieza `pending/unknown`; el primer snapshot
valido lo cambia a `active/healthy` o `active/degraded`. Host `version` incrementa con todo cambio visible: pull,
snapshot/filesystems/warnings/estado, stale persistido en su deadline, unreachable y
configuracion futura. Esto hace strong al ETag; futuro If-Match puede competir con
polling y devolver 412.

Resumen muestra tarjetas de hosts y `/hosts/:id` muestra metricas, frescura,
Tailscale, warnings y filesystems. Refresca cada 30 segundos sin solapamiento, pausa
cuando el documento esta oculto y refresca al volver. Estados: healthy verde,
degraded/stale amarillo, unreachable rojo y unknown gris. Debe funcionar con teclado,
`prefers-reduced-motion` y viewport de 320 px.

## 6. Verificacion

- Fixtures Linux cubren calculos, overflow, escapes de mountinfo, UUID/fallback,
  pseudo-filesystems y fallos parciales.
- Tests del agente cubren YAML, permisos, bearer, rate limit, concurrencia, warnings y
  redaccion.
- Tests de red cubren TLS/SNI, DNS mixto/cambiante, IPv4/IPv6, redirects, proxy,
  timeout, body excesivo y protocolo invalido.
- Tests SQLite/scheduler usan reloj y random inyectados para cadencia, jitter, leases,
  restart, tercer deadline, stale, cambios de intervalo, no catch-up y dos workers.
- Tests de crash cubren cada frontera de transacciones y archivos del bootstrap.
- Vue y Playwright cubren lista/detalle, polling, estados, cero versus ausencia,
  expiracion de sesion, teclado y 320 px.
- Verificacion final ejecuta `make lint`, `make test`, `make build` y `make test-e2e`,
  mas smoke en Oracle arm64 con bootstrap repetido, agente real y transicion a
  unreachable/recuperacion.

## 7. Supuestos operativos

Tailscale ya esta instalado y conectado, existe FQDN `.ts.net` y el operador configura
Serve 9474 manualmente. Directorios padre y propietarios se aprovisionan manualmente
en este slice. No se agrega dependencia Go nueva. Builds locales pueden informar
version `dev`; releases usan semver.

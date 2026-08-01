# Plan de implementacion

Plan ordena trabajo para reducir riesgo. No reemplaza requisitos de `spec.md`.

## 1. Forma de trabajo

- Completar fases en orden salvo tareas puramente documentales independientes.
- Cada cambio debe dejar rama compilable y pruebas existentes verdes.
- Preferir slices verticales pequenos sobre crear todas las capas vacias.
- Una fase termina solo al cumplir condiciones de salida, no al crear archivos.
- No implementar items futuros mientras existan criterios v1 pendientes.
- Cambios de contrato requieren actualizar documentos y pruebas en mismo cambio.

Durante fase 0, establecer comandos estables:

```bash
make test
make lint
make build
make test-e2e
```

Fase 0 tambien fija Go `1.26.5`, Node.js LTS `24.18.0`, npm `11.16.0` y
`create-vue 3.22.4` mediante `.tool-versions`, `packageManager` y lockfile npm. Usa
Vitest para tests frontend, Playwright para E2E, y ESLint con
`eslint-plugin-vue` mas Prettier para lint y formato frontend. El CI en GitHub Actions
usa las versiones fijadas y `make` delega a los mismos comandos
locales. Si el proyecto cambia de task runner, conserva equivalentes documentados en
README.

Dependencias Go iniciales quedan limitadas a `modernc.org/sqlite v1.53.0`,
`golang.org/x/crypto v0.54.0` y `go.yaml.in/yaml/v3 v3.0.4`. Se fijan en `go.mod`, se
revisan licencias y se prueba build `CGO_ENABLED=0` para ambas arquitecturas. Mientras
Fase 0 aun no las consume en produccion, una prueba de compatibilidad abre SQLite en
memoria, deriva Argon2id y decodifica YAML con campos conocidos estrictos. `make build`
compila esa prueba para Linux `amd64` y `arm64` con `-exec=true`, sin ejecutar el
binario cruzado ni conservarlo como entregable.

## 2. Estructura inicial sugerida

```text
cmd/atalaya/
internal/
  agent/
  alerts/
  auth/
  checks/
  collection/
  config/
  crypto/
  database/
  httpapi/
  notifications/
  retention/
  server/
web/
  src/
docs/
scripts/
```

No crear todos los paquetes vacios. Agregarlos cuando primera funcionalidad los use.

## 3. Fase 0 - Bootstrap reproducible

Objetivo: compilar frontend y backend en ejecutable unico reproducible.

Trabajo:

- Inicializar modulo Go `github.com/mkrebs03/atalaya` y aplicacion Vue
  3/TypeScript/Vite.
- Configurar TypeScript estricto, lint y formato.
- Crear CLI con `server`, `agent` y `version`, aunque agent solo pueda mostrar error
  de no implementado durante esta fase.
- Reservar comandos `backup` y `verify` con ayuda y codigos de salida estables.
- Fijar codigos CLI: `0` para exito/ayuda/version, `1` para fallo operativo o comando
  reservado no implementado y `2` para uso/configuracion invalida.
- Embebir `web/dist` y servir pagina inicial desde Go. El frontend se compila antes de
  tests, vet y builds Go porque `go:embed` exige que el directorio exista.
- Agregar endpoint publico `/healthz`.
- Agregar config de desarrollo con proxy Vite a Go y modo `development` explicito. La
  cookie puede omitir `Secure` solo con listener y origen HTTP en loopback; cualquier
  otro entorno falla startup.
- Limitar el listener HTTP Go a loopback en todos los modos. Produccion exige origen
  HTTPS y development permite origen HTTP solo en loopback; errores de URL no repiten
  el valor recibido.
- Agregar tests unitarios base y smoke test del binario.
- Configurar build cruzado Linux `amd64`/`arm64`.

Condiciones de salida:

- Checkout limpio puede ejecutar tests y build mediante comandos documentados.
- Ejecutable sirve assets Vue sin Node.js instalado en runtime.
- `/healthz` no revela detalles internos.
- CI ejecuta lint, tests y build para cada cambio.

La matriz Tailscale se difiere hasta la fase de instalacion y release, cuando existan
la deteccion de capacidades, unidades y rutas 443/9474 que debe comprobar. Fase 0 no
publica un job ficticio sin comportamiento Tailscale implementado.

## 4. Fase 1 - Persistencia y autenticacion

Objetivo: servidor seguro con SQLite, migraciones y administrador local.

Trabajo:

- Abrir SQLite con pragmas definidos y migraciones versionadas.
- Implementar clave maestra y cifrado versionado con tests.
- Implementar `admin create` y `admin reset-password`.
- Implementar Argon2id, sesiones hasheadas, cookies y CSRF.
- Persistir `remote_addr` observado y metadata proxy solo como informacion nullable,
  sin atribuir una IP tailnet ni usarla para auth.
- Crear login, logout, `auth/me` y cambio de password.
- Proteger rutas API y fallback SPA.
- Agregar layout Vue autenticado y manejo global de 401/403.
- Agregar headers de seguridad, `Cache-Control: no-store` en autenticacion y respuestas
  con secretos efimeros, y request IDs.

Condiciones de salida:

- Base vacia migra pero permanece sin inicializar hasta ejecutar `atalaya admin create`;
  comando crea un unico administrador con password suministrado interactivamente.
- Usuario no autenticado solo accede assets, login y `/healthz`.
- Reset revoca sesiones.
- Tests cubren expiracion, CSRF, cache headers, atributos cookie en produccion y modo
  development limitado a loopback, y redaccion de errores.
- Clave incorrecta contra DB existente falla startup sin modificar datos.

## 5. Fase 2 - Agente, enrolamiento y estado actual

Objetivo: Oracle y segundo host pueden entregar snapshots autenticados.

### Fase 2A - Agente local y estado actual

Primer slice: Oracle visible mediante agente local, bootstrap idempotente, scheduler
persistente, endpoints read-only y resumen/detalle. Usa intervalo 60 segundos, timeout
10 segundos, jitter 0-5 segundos, lease con 5 segundos de margen, concurrencia global
4 y payload 256 KiB. Incluye warnings parciales, clock skew de cinco minutos, red
restringida a FQDN `.ts.net:9474` y filesystems elegibles seleccionados por default.

No incluye enrolamiento remoto, mutaciones de hosts/filesystems, historico, systemd ni
instalador. El contrato detallado esta en el
[diseno de Fase 2A](superpowers/specs/2026-07-17-phase-2a-local-agent-current-state-design.md).

Condiciones de salida de 2A:

- Bootstrap repetido conserva una unica identidad y credencial, y recupera de fallos
  entre transacciones/escrituras sin reemplazar archivos validos.
- Oracle aparece con snapshot actual real, filesystems y warnings en resumen/detalle;
  bearer invalido responde 401 sin filtrar datos.
- Scheduler sobrevive restart, no hace catch-up y muestra stale/unreachable con los
  deadlines persistidos; no persiste `MetricSample` ni historico raw.
- Agent URL, DNS, TLS, redirects, timeout y payload cumplen el contrato de seguridad;
  agente funciona sin root.
- `make lint`, `make test`, `make build` y `make test-e2e` pasan.

### Fase 2B - Enrolamiento y gestion

Completa tokens, enrolamiento de segundo host y mutaciones de hosts/filesystems.

Trabajo:

- Crear tokens base64url de 32 bytes, persistir solo su hash y mostrarlos una vez en
  la UI; listar metadatos y revocar con versionado.
- Implementar `POST /api/v1/agent-enrollments` con TTL de 15 minutos, replay exacto
  de 15 minutos, request ID persistido en el agente y credencial 0600.
- Implementar `atalaya agent enroll --config <path>` con prompt o archivo temporal
  0600; el token no entra por argumentos ni variables de entorno.
- Gestionar nombre, intervalo 15..3600, timeout 1..60, activar/desactivar,
  archivar/restaurar y selección de filesystems mediante CSRF e `If-Match`.
- Exponer una vista administrativa que distinga hosts archivados y estados de token,
  y documentar los campos de cadencia/archivado en el contrato de hosts.

Condiciones de salida de 2B:

- Reintentar una respuesta perdida no crea un segundo host ni rota la credencial; un
  replay alterado, expirado o revocado falla sin filtrar secretos.
- El servidor nunca persiste el token en claro; el agente y la UI no lo guardan en
  almacenamiento del navegador, logs, argv ni entorno.
- Toda mutación existente sin ETag responde 428 y con ETag obsoleto responde 412;
  sin CSRF responde 403.
- Cambiar una cadencia cierra el periodo anterior y ancla uno nuevo sin modificar
  slots históricos; desactivar o archivar cancela la programación.
- Restaurar deja el host desactivado; habilitarlo después requiere una acción
  explícita y los hosts archivados no aparecen en listas por defecto.
- `make lint`, `make test`, `make build` y `make test-e2e` pasan.

### Alcance acumulado de Fase 2, completado despues de 2B

Trabajo acumulado (2A + 2B):

- Implementar recoleccion Linux de CPU, memoria, uptime, boot ID y filesystems.
- Implementar consulta segura del estado Tailscale.
- Implementar filtro de pseudo-filesystems y tests con fixtures Linux.
- Recolectar UUID, mount root y major:minor cuando Linux los exponga; implementar la
  identidad UUID-first y fallback sin fusion heuristica.
- Implementar listener del agente y bearer por agente.
- Implementar tokens de enrolamiento desde API/UI.
- Implementar enrolamiento idempotente y persistencia `0600` en agente; el token
  ingresa por prompt o archivo temporal eliminado, nunca por argv.
- Implementar scheduler pull, timeouts, jitter y limite de concurrencia.
- Capturar intervalo/timeout por schedule, limitar jitter a 5 segundos y actualizar
  `freshness_deadline_at` solo tras exito desde el siguiente deadline mas 5 segundos.
- Persistir `SchedulePeriod` y un `ScheduleSlot` no terminal por target con identidad,
  tiempos efectivos, lease y estado. Resultado scheduled, finalizacion y creacion del
  siguiente slot comparten transaccion y unicidad por `slot_id`.
- Mantener cadencia nominal fija dentro de cada periodo; cancelar con
  `missed_deadline` y reanclar un periodo nuevo cuando downtime supera el deadline.
- Crear el comando idempotente `atalaya bootstrap local-agent`, independiente del
  instalador, para crear host Oracle, credencial cifrada y config local sin token UI.
- Persistir snapshot actual, fallos consecutivos y estado derivado.
- Crear resumen y detalle de hosts responsive.
- Permitir seleccionar/ignorar filesystems y activar/desactivar hosts.

Condiciones de salida acumuladas, exigibles despues de 2B:

- Token expirado, revocado o reutilizado indebidamente no enrola host.
- Snapshot sin credencial valida responde 401 sin filtrar datos.
- Pull exitoso muestra datos reales; tres fallos operativos programados y el deadline del tercer
  slot fallido muestran unreachable.
- Payload invalido cierra/reprograma el slot, pero no incrementa fallos consecutivos ni
  cambia estado/version/timestamps actuales, snapshot o filesystems.
- Ultimo valor stale aparece advertido, no como actual.
- Agente funciona sin root en distribucion Linux objetivo.
- Payload malicioso o excesivo se rechaza sin afectar scheduler.
- Cambiar intervalo no altera frescura ni deadlines de schedules ya creados.
- Restart recupera el slot persistido antes de su deadline sin duplicar resultado. Un
  slot ya vencido no inicia red: queda como hueco esperado, excluye nominales
  intermedios y crea un periodo nuevo sin catch-up.
- Desactivar durante una ejecucion cancela su slot al finalizar, sin actualizar estado,
  series o alertas ni reactivar scheduling.
- Cambio de nombre de dispositivo con UUID estable conserva filesystem e historico;
  fallback cambiado crea otra entidad sin mezclar series.
- `atalaya bootstrap local-agent` puede repetirse sin duplicar host ni rotar secreto.

## 6. Fase 3 - Series, graficos y retencion

Objetivo: historico confiable con huecos y rollups.

Trabajo:

- Persistir muestras crudas normalizadas durante pull.
- Crear consultas por rango y resolucion automatica, incluyendo `resolution=mixed` y
  resolucion explicita por punto al cruzar el cutoff raw.
- Rechazar `raw` fuera de retencion con `raw_not_available` y calcular hourly en
  lectura para horas abiertas o aun no agregadas que conservan raw.
- Definir buckets horarios semiabiertos, cobertura y limite de 10000 puntos por serie.
- Acotar el trabajo de consultas `hourly`/`auto` antes de iterar buckets, incluso cuando
  la ventana no contiene muestras.
- Consumir los periodos de schedule para calcular cobertura cuando cambia el intervalo.
- Separar agregacion por `received_at` de cobertura por slot nominal y `slot_id`; no
  inferir `complete` comparando `sample_count` con expected.
- Implementar rollups horarios idempotentes.
- Implementar retencion en lotes y marcas de progreso.
- Exponer API de metricas con limites de rango/puntos.
- Integrar `uPlot` mediante componente Vue con teardown correcto.
- Mostrar rangos 24h, 7d, 30d y 90d.
- Mostrar huecos sin interpolacion engañosa.
- Persistir y editar desde UI la retencion raw/rollup mediante el recurso singleton de
  settings; la mutacion usa version/ETag y CSRF.

Condiciones de salida:

- Reiniciar job no duplica rollups ni elimina datos no agregados.
- Consulta que cruza frontera raw/rollup no duplica puntos.
- Cambio de intervalo dentro de una hora calcula cobertura por segmentos; si falta
  historia, omite expected count y marca rollup parcial.
- Muestra que cruza frontera horaria agrega su valor por `received_at`, satisface el
  slot nominal correcto y no completa o incompleta el bucket equivocado.
- Consulta que supera 10000 puntos en cualquier serie responde 422 sin truncar.
- Consulta raw fuera del cutoff no devuelve una ventana parcial; hora abierta hourly
  queda marcada incompleta y no se persiste como rollup definitivo.
- Grafico diferencia ausencia de dato de valor cero.
- 90 dias con defaults permanece dentro de limites de respuesta definidos.
- Tests usan reloj inyectable; no dependen de esperas reales.

## 7. Fase 4 - Checks HTTP y TCP

Objetivo: servicios configurables y observados desde servidor.

Estado: implementada en el bootstrap v1. La migración 007, el API autenticado, los
workers del servidor y las vistas de lista/detalle cubren este corte; alertas y
notificaciones permanecen fuera de alcance hasta la Fase 5.

Trabajo:

- Implementar modelo/API/UI CRUD tipado de checks.
- Implementar workers y scheduler compartiendo patrones con collection sin acoplar
  dominios.
- Implementar HTTP con status esperado, timeout y redirects limitados.
- Implementar TCP connect.
- Aplicar validacion SSRF antes de cada conexion/redirect; solo Tailscale y CIDRs
  privados autorizados, nunca destinos publicos por default.
- Persistir resultados scheduled por `slot_id` y runs manuales en `CheckRun`; manual
  no cambia estado, series ni alertas.
- Reclamar `CheckRun` con lease de timeout mas margen, maximo dos intentos de proceso y
  recuperacion tras restart; capturar configuracion inmutable, permitir run desactivado
  y rechazar archivado.
- Exponer series scheduled de availability/latency con unidades y contadores definidos,
  y listar runs manuales por separado sin resolucion ni rollups.
- Crear vistas de lista, detalle y ejecucion manual asincrona.

Condiciones de salida:

- Checks reales up/down actualizan UI e historico.
- Timeout incluye DNS, connect, TLS y respuesta.
- Metadata cloud, loopback, destinos publicos y redirects a destinos bloqueados se
  rechazan.
- Cuerpo HTTP y URLs sensibles no se persisten ni registran.
- Desactivar check detiene scheduling sin borrar historico.
- `run` devuelve ID consultable, es idempotente por `request_id` y los resultados
  manuales quedan separados.
- Lease vencido de run no lo deja en `running`; se recupera o termina
  `retry_exhausted`, manteniendo el mismo ID.
- Availability raw usa 0/1 y hourly coincide con exitos/total; fallos no generan
  latencia cero.

## 8. Fase 5 - Alertas y Discord

Objetivo: incidentes deduplicados notifican apertura y resolucion.

Estado: implementada en el bootstrap v1. La migración 008, la evaluación posterior a
resultados programados, el outbox con worker Discord, el API autenticado y las vistas
de reglas, incidentes, canales y entregas cubren este corte.

Trabajo:

- Implementar reglas host unreachable, check failed y thresholds.
- Implementar maquina `inactive/pending/firing` y cierre de incidentes.
- Persistir `state=open|resolved` y `condition_status=true|false|unknown` en incidentes.
- Crear UI CRUD para reglas y vistas de incidentes.
- Implementar canales cifrados y adaptador Discord.
- Validar webhooks Discord mediante URL parseada, host y path exactos, sin userinfo,
  query, fragment ni puerto no estandar.
- Implementar outbox, reintentos con backoff, deduplicacion, destinos capturados al
  abrir incidente y estado terminal `skipped`.
- Implementar entregas `kind=incident|test`, idempotencia de test por
  canal/`test_request_id` y test asincrono.
- Revalidar estado del canal en la transaccion de claim; canales inactivos convierten
  pendientes en `skipped` antes de iniciar red.
- Persistir payload final inmutable y enviar Discord sin redirects, con timeout total
  de 15 segundos menor al lease de 60 segundos.
- Mostrar warning/critical en semaforo global.

Condiciones de salida:

- Pico corto no abre incidente cuando regla exige duracion.
- Condicion sostenida abre exactamente un incidente.
- Recuperacion cierra mismo incidente y genera una entrega por canal.
- Canales agregados a mitad de incidente no reciben solo la resolucion; canales
  inactivos producen entregas `skipped` auditables sin bloquear el orden.
- Repetir un test con mismo request ID no duplica entrega; archivar un canal detiene
  pendientes no reclamadas y no cancela a mitad un request ya `sending`.
- Reinicio entre persistencia y envio no pierde ni duplica entrega logica.
- Redirect de Discord no produce un segundo egress y un request nunca supera el lease;
  reintentos conservan exactamente el mismo payload.
- Webhook no aparece en API, DB en claro, logs ni errores.
- Fallo de Discord no bloquea nuevas evaluaciones.

## 9. Fase 6 - Operacion e instalador

Objetivo: instalar, actualizar, respaldar y restaurar sin toolchain de desarrollo.

Trabajo:

- Crear units `systemd` con hardening para server y agent.
- Crear instalador idempotente con roles y verificacion de checksum.
- Agregar `--no-start` para restore y migracion offline antes de `verify`.
- Integrar deteccion/configuracion guiada de Tailscale Serve en 443/9474; verificar
  capacidades y rutas contra la matriz publicada por release, sin fallback silencioso
  de puerto.
- Hacer que el rol server invoque `atalaya bootstrap local-agent` antes de instalar la
  unidad local del agente.
- Crear comandos `backup`, `backup-key`, `verify` y `migrate`; backup de datos excluye
  la clave y la restauracion exige su artefacto separado.
- Crear `atalaya verify` para validar config, schema, acceso DB y descifrado antes de
  confirmar upgrade.
- Documentar restore offline en orden `migrate -> verify -> start`.
- Crear flujo de upgrade con backup y rollback de binario.
- Publicar artefactos `amd64`/`arm64` y checksums desde CI.
- Documentar paths, firewall, logs, backup y limitacion watchdog.
- Agregar pagina autenticada de estado del sistema.

Condiciones de salida:

- VM Linux limpia con Tailscale conectado instala server mediante un comando.
- Segundo host instala agent y se enrola sin Go/Node/Docker.
- CLI Tailscale fuera de la matriz o sin rutas 443/9474 concurrentes falla con version
  detectada y capacidad ausente.
- Reejecutar instalador no rota claves ni borra config.
- Backup restaurado conserva login, metricas, reglas y descifra canales.
- Upgrade fallido restaura como unidad binario y backup pre-migracion; nunca combina
  binario viejo con schema incompatible.
- Upgrade solo descarta rollback despues de `/healthz` y `atalaya verify` exitosos.
- `atalaya backup` crea backup consistente y la restauracion documentada pasa `verify`
  antes de iniciar.
- Backup de datos no contiene `master.key`; export de clave preserva `0600` y rechaza
  compartir output con el archivo de datos.
- Servicios no ejecutan como root y reinician tras reboot.

Estado de implementacion de Fase 6: completados el CLI de backup/backup-key/verify,
snapshot SQLite consistente con VACUUM INTO, verificacion read-only del schema vigente,
exclusion de la clave maestra, adaptador de capacidades Tailscale Serve, instalador
idempotente, units endurecidas, flujo de rollback, endpoint autenticado de estado,
vista /system, workflow de GitHub Releases y documentacion operativa. La evidencia
reproducible queda en make lint, make test, make build y make test-e2e; la prueba de
VM Linux con Tailscale conectado sigue siendo una comprobacion de entorno de release,
no una dependencia del runtime.

## 10. Fase 7 - Cierre v1

Objetivo: validar contrato completo y preparar release estable.

Trabajo:

- Ejecutar todos los criterios de aceptacion de `spec.md` en entorno representativo.
- Pruebas responsive y teclado en navegadores objetivo.
- Prueba de carga moderada con cantidad documentada de hosts, filesystems y checks.
- Revisar accesibilidad automatizada y manual basica.
- Revisar logs y DB buscando secretos de fixtures reales controlados.
- Probar migracion desde primera release candidata.
- Corregir documentacion usando instalacion desde cero, no memoria del desarrollador.

Condiciones de salida:

- Los 15 criterios de aceptacion tienen evidencia o prueba automatizada.
- No quedan findings de seguridad high/critical conocidos.
- Limitaciones conocidas aparecen en UI/documentacion apropiada.
- Release puede instalarse y restaurarse siguiendo solo documentacion publicada.

## 11. Estrategia de pruebas

### Unitarias

- Parsers Linux, calculos de CPU/memoria y filtros de filesystem.
- Maquinas de estados y thresholds.
- Slots, cadencia nominal, deadlines vencidos, reanclaje, leases, unicidad por `slot_id`
  y cambios de intervalo.
- Claims y recuperacion de `CheckRun`, incluyendo archive y agotamiento de intentos.
- Crypto, tokens, redaccion y validacion SSRF.
- Rollups por segmentos, fronteras temporales, `received_at`, intervalo efectivo y
  expiracion de frescura.
- Archivado/restauracion y transiciones de incidentes con condicion `unknown`.

### Integracion

- SQLite real temporal con migraciones.
- HTTP handlers con servidor de prueba.
- Agente y servidor con certificados/control de auth.
- Enrolamiento local de Oracle y replay exacto sin duplicar host o credencial.
- Identidad de filesystem con UUID y fallback ante device/mount changes.
- Discord mediante fake server que capture payloads.
- Entregas de test sin incidente, replay idempotente y canal archivado antes del claim.
- Discord sin redirects, timeout menor al lease y payload inmutable entre reintentos.
- URLs Discord malformadas o con host parecido se rechazan antes de cualquier egress.
- DNS/dialers inyectados para validar SSRF sin depender de red publica.
- Ejecuciones manuales separadas de resultados programados y operaciones `202` con ID.
- Restart durante `CheckRun` recupera lease sin dejar estados `running` permanentes.

### Frontend

- Componentes de estados criticos y formularios.
- Composables API con errores 401/409/422.
- Respuestas mixtas raw/hourly y error `point_limit_exceeded`.
- Errores `raw_not_available`, hourly abierto y runs manuales separados.
- Graficos con huecos y cambio de rango.
- E2E para login, enrolamiento simulado, check, regla e incidente.

### Sistema

- Units `systemd` en VM/container de CI compatible cuando sea posible.
- Instalacion y upgrade sobre directorios temporales o VM efimera.
- Backup/restauracion con clave separada, `verify` y rollback con schema incompatible.
- Matriz Tailscale prueba version estable y mas antigua soportada por cada release.
- Build y smoke test de ambas arquitecturas; ejecucion nativa donde CI lo permita.

## 12. Trabajo futuro separado

No mezclar con cierre v1:

- Watchdog en segundo host.
- Checks locales especificos como presencia de sesion tmux.
- Nuevos proveedores de notificacion.
- Paquetes distro.
- Multiusuario o publicacion general.

Cada item futuro necesita spec y criterios propios antes de implementacion.

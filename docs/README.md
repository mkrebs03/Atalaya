# Documentacion tecnica

## Indice

- [Arquitectura](architecture.md): stack, componentes, flujos y limites.
- [Modelo de dominio y datos](domain-model.md): entidades, estados, persistencia y
  retencion.
- [API y protocolos](api.md): contratos HTTP entre UI, servidor y agentes.
- [Seguridad y operacion](security-and-operations.md): amenazas, secretos, despliegue,
  backup y actualizacion.
- [Plan de implementacion](implementation-plan.md): fases, dependencias y condiciones
  de salida.
- [Dependencias de runtime y fundacionales](dependencies.md): versiones directas y
  licencias.

## Autoridad

Orden de precedencia ante contradicciones:

1. `spec.md` para comportamiento visible, alcance y criterios de aceptacion.
2. `docs/security-and-operations.md` para controles de seguridad.
3. `docs/api.md` para contratos entre procesos y frontend.
4. `docs/domain-model.md` para semantica y persistencia.
5. `docs/architecture.md` para organizacion interna y stack.
6. `docs/implementation-plan.md` para secuencia de trabajo, no comportamiento.

Si dos requisitos del mismo nivel se contradicen, detener implementacion y actualizar
documentacion mediante una decision explicita. No elegir silenciosamente.

## Reglas para agentes

- Implementar solamente alcance de fase o tarea asignada.
- No agregar servicios de infraestructura sin modificar primero arquitectura.
- No cambiar defaults normativos sin actualizar `spec.md`.
- No exponer secretos en API, logs, errores o fixtures.
- Escribir migraciones compatibles hacia adelante; nunca editar una migracion ya
  publicada.
- Agregar pruebas sobre comportamiento, no detalles internos accidentales.
- Mantener Linux `amd64` y `arm64` como plataformas soportadas.
- Preferir biblioteca estandar de Go. Agregar dependencia cuando reduzca riesgo o
  complejidad total de forma demostrable.
- Mantener frontend accesible, responsive y consistente con estados de dominio.
- Registrar decisiones nuevas en documento correspondiente antes o junto al codigo.

## Estado de decisiones

Decisiones cerradas:

- Go para servidor, agente y CLI.
- Modulo Go canonico `github.com/mkrebs03/atalaya`; el nombre publico del producto
  puede cambiar sin renombrar imports.
- GitHub Actions como CI para lint, tests, build y matriz Tailscale.
- Vue 3, TypeScript y Vite para frontend.
- SQLite como unica base de datos v1.
- `systemd` para procesos Linux.
- Tailscale como red privada y Tailscale Serve para HTTPS del dashboard.
- Pull de metricas, intervalo por defecto de 60 segundos.
- Usuario administrador unico con auth local.
- Discord como primer adaptador de notificacion.
- `received_at` del servidor es el unico timestamp de muestra para series y alertas;
  frescura expira segun `freshness_deadline_at`, capturado solo tras un exito.
- Scheduling persistente: `SchedulePeriod` conserva la historia usada por cobertura y
  `ScheduleSlot` identifica cada ejecucion emitida; cambios de configuracion no
  reinterpretan slots existentes.
- Cada periodo usa cadencia nominal fija. Slots que superaron su deadline durante
  downtime se cancelan y un periodo nuevo se ancla al presente, sin catch-up.
- `unreachable` requiere tres fallos programados consecutivos y alcanzar el deadline
  del tercer slot fallido.
- `DELETE` archiva siempre los recursos configurables; no hay borrado fisico desde la
  UI.
- Los checks solo alcanzan Tailscale y CIDRs privados autorizados; destinos publicos
  estan bloqueados por default.
- Incidentes separan `state` (`open`/`resolved`) de `condition_status`
  (`true`/`false`/`unknown`).
- Abrir un incidente captura el snapshot de canales destino; entregas no elegibles
  terminan en estado terminal `skipped` auditable.
- Entregas distinguen `incident` y `test`; el worker vuelve a validar el canal al
  adquirir el lease.
- Webhooks Discord se validan con parser estructurado: host y path exactos, sin
  userinfo, query, fragment ni puerto no estandar.
- Puerto HTTPS 9474 fijo para agentes via Tailscale Serve; una CLI sin las capacidades
  requeridas falla con diagnostico, sin fallback silencioso de puerto.
- Dependencias Go fundacionales: `modernc.org/sqlite`, `golang.org/x/crypto/argon2` y
  `go.yaml.in/yaml/v3`, con builds `CGO_ENABLED=0`.
- La instalacion server en Oracle configura el agente local mediante el comando
  idempotente `atalaya bootstrap local-agent`.
- Migraciones se aplican al arrancar el servidor; `atalaya migrate` ejecuta las mismas
  sin arrancarlo.
- Modo `development` explicito; cookie sin `Secure` solo con listener y origen en
  loopback.
- `Cache-Control: no-store` en autenticacion y respuestas con secretos efimeros.
- Respuestas de metricas declaran resolucion por punto, usan `mixed` al combinar
  fuentes y limitan a 10000 puntos por serie con `422`.
- Backups de datos excluyen la clave maestra; su exportacion y custodia son separadas.
- Toolchain de build fijado por archivos de version y lockfiles.

No decidido porque no bloquea inicio:

- Nombre publico definitivo.
- Proveedor de releases mientras proyecto siga privado.
- Proveedores de notificacion posteriores a Discord.

# Atalaya - Especificacion de producto

Estado: aprobado para implementacion de v1.

Este documento define que debe hacer el producto. Las decisiones de arquitectura,
datos, API, seguridad y despliegue viven en [`docs/`](docs/README.md).

Las palabras **DEBE**, **NO DEBE**, **DEBERIA** y **PUEDE** expresan requisitos
normativos. Un agente no debe ampliar el alcance sin actualizar primero esta
documentacion.

## 1. Vision

Atalaya es un dashboard personal para observar hosts Linux y servicios
self-hosted accesibles por una red Tailscale. Debe ofrecer estado actual, historico,
alertas y configuracion desde una interfaz web, con operacion sencilla y pocos
componentes.

El sistema comienza en una VM Oracle y debe permitir agregar hosts y servicios sin
refactorizar su arquitectura. Es un proyecto personal; una posible publicacion futura
no justifica agregar multiusuario ni abstracciones innecesarias en v1.

## 2. Principios

- Instalacion y actualizacion simples: un ejecutable, SQLite y `systemd`.
- Sin Docker, Node.js, Go, proxy web adicional como nginx/Caddy o base de datos
  externa en runtime. Tailscale Serve es parte requerida de conectividad Tailscale.
- Todo acceso operativo ocurre dentro de Tailscale. Unica excepcion: egress de
  notificaciones hacia proveedores externos configurados explicitamente, como el
  webhook de Discord.
- Tailscale limita exposicion de red, pero no reemplaza autenticacion propia.
- Los checks solo pueden alcanzar destinos de la tailnet o CIDRs privados autorizados
  explicitamente; destinos publicos quedan bloqueados por default.
- Configuracion cotidiana desde UI, no editando archivos o tablas manualmente.
- Estado desconocido o desactualizado nunca se presenta como saludable.
- Huecos de datos deben mostrarse como huecos, no interpolarse como mediciones reales.
- Defaults seguros y utiles; opciones avanzadas solo cuando exista necesidad concreta.

## 3. Usuarios y entorno

- Un unico usuario administrador.
- Todos los hosts monitoreados usan Linux y permanecen normalmente encendidos.
- La VM Oracle aloja inicialmente servidor, frontend y un agente local.
- Navegadores objetivo: versiones actuales de Firefox, Chrome y Safari, en escritorio
  y telefono.
- La aplicacion no se expone directamente a Internet publico.

## 4. Alcance v1

### 4.1 Hosts

El sistema DEBE:

- Monitorear inicialmente la VM Oracle.
- Permitir registrar otros hosts desde la UI mediante tokens de enrolamiento de un
  solo uso y duracion limitada.
- Obtener snapshots de cada agente cada 60 segundos por defecto.
- Mostrar CPU, memoria, filesystems, uptime y estado observado de Tailscale.
- Descubrir filesystems automaticamente, excluyendo filesystems virtuales y
  pseudo-filesystems. El administrador puede habilitar o ignorar mounts desde UI.
- Conservar ultimo snapshot y momento de ultima comunicacion exitosa.
- Considerar dato stale cuando `now` supera `last_success_received_at + intervalo
  efectivo + timeout efectivo + jitter maximo + 5 segundos de margen operativo`.
  Intervalo y timeout se capturan para cada schedule y no cambian retroactivamente.
  `received_at` es asignado por el servidor y es la unica referencia para frescura,
  series y alertas.
- Marcar un host como no disponible cuando existan tres fallos programados completados
  y se haya alcanzado el deadline del tercer slot fallido, calculado con el intervalo
  efectivo de cada schedule. Para un host nunca observado, el primer slot comienza en
  el enrolamiento. El jitter del scheduler esta acotado a 5 segundos y es adicional
  al margen operativo.
- Permitir desactivar monitoreo de un host sin eliminar su historico.
- Permitir archivar y restaurar un host. Archivar detiene el polling y conserva
  historico, incidentes y referencias.

Un agente expone datos; el servidor central inicia la recoleccion. La comunicacion de
control usada para enrolamiento no convierte el modelo de metricas en push.

### 4.2 Servicios

El sistema DEBE permitir crear, editar, desactivar, archivar y restaurar checks desde
UI. Archivar es una baja logica y nunca elimina historico.

Tipos v1:

- HTTP/HTTPS: metodo `GET`, intervalo y timeout configurables, rango esperado
  configurable y valor por defecto 200-299. Redirects estan desactivados por default;
  si se habilitan, el limite es configurable de 1 a 3, con tope duro de 3 en v1.
- TCP: apertura de conexion a host y puerto con timeout configurable.

Los checks se ejecutan desde el servidor central para representar disponibilidad
desde la tailnet, no solamente desde localhost del host destino. Solo pueden usar
destinos Tailscale o CIDRs privados autorizados desde settings; loopback, link-local,
multicast, metadata y destinos publicos quedan bloqueados. Cada resultado incluye
estado, latencia, fecha y error normalizado sin secretos.

### 4.3 Dashboard

La UI DEBE incluir:

- Resumen de hosts con estado, CPU, memoria, disco mas comprometido y ultima muestra.
- Detalle de host con valores actuales, filesystems y graficos historicos.
- Lista y detalle de servicios con estado, latencia y ultimo error.
- Gestion de hosts, checks, reglas de alerta y canales de notificacion.
- Actualizacion automatica sin recargar pagina completa.
- Indicadores explicitos para `unknown`, datos desactualizados y huecos historicos.
- Diseño usable con teclado y en pantallas moviles.

Semaforo de resumen:

- Verde: datos recientes y ninguna alerta activa.
- Amarillo: regla pendiente, alerta warning activa o degradacion parcial.
- Rojo: host no disponible o alerta `critical` activa.
- Gris: nunca observado, desactivado o estado todavia desconocido.

Si coinciden estados, prevalece rojo, luego amarillo, verde y gris.

### 4.4 Historico

El sistema DEBE:

- Guardar series por host para CPU, memoria y filesystems.
- Guardar estado y latencia por check de servicio.
- Retener muestras crudas durante 48 horas por defecto.
- Generar agregados horarios y retenerlos 90 dias por defecto.
- Mostrar 24 horas inicialmente y permitir consultar 7, 30 y 90 dias.
- Ejecutar retencion y agregacion sin intervencion manual.
- Mantener huecos cuando servidor o agente no recolectaron datos.

Retencion e intervalo son configurables, pero esos valores son defaults soportados y
probados. La retencion de rollups nunca puede ser menor que la retencion raw. Las
consultas usan ventanas semiabiertas y no duplican puntos en la frontera raw/rollup.
v1 no requiere buffering local: si servidor central se detiene, se pierden las
muestras de ese periodo.

### 4.5 Alertas

Tipos de regla v1:

- Host sin responder durante un periodo configurable.
- Check de servicio fallido durante un periodo o numero de intentos configurable.
- CPU o memoria por encima de un umbral durante tiempo sostenido.
- Filesystem por encima de un umbral configurable.

Cada evaluacion de alerta sigue el ciclo `inactive -> pending -> firing`. La
resolucion cierra el incidente asociado; no es un estado de evaluacion.

El sistema DEBE:

- Evitar alertas por picos que no cumplen duracion configurada.
- Deduplicar notificaciones del mismo incidente.
- Registrar inicio, cambios relevantes y resolucion.
- Notificar la apertura y resolucion del incidente.
- Permitir severidad `warning` o `critical`.
- Conservar historial de incidentes aunque se archive o desactive la regla.
- Permitir archivar y restaurar reglas. Archivar detiene evaluaciones y resuelve el
  incidente abierto con razon `rule_archived`.

### 4.6 Notificaciones

- Discord mediante webhook es el primer proveedor obligatorio.
- La arquitectura DEBE permitir varios proveedores y varios destinos por proveedor.
- Una regla puede notificar a uno o varios destinos simultaneamente.
- Fallar al enviar una notificacion NO DEBE bloquear evaluacion ni persistencia de
  alertas.
- Intentos y errores de entrega deben quedar registrados.
- Webhooks y credenciales reutilizables se almacenan cifrados. La URL se valida con un
  parser contra el contrato del proveedor; Discord solo acepta HTTPS, host exacto
  `discord.com` y path exacto `/api/webhooks/{id}/{token}`, sin userinfo, query,
  fragment ni puerto distinto de 443.
- Permitir archivar y restaurar canales. Un canal archivado no recibe entregas nuevas,
  pero conserva su historial.

### 4.7 Autenticacion

- Existe un unico administrador en v1.
- El primer administrador se crea mediante comando local de bootstrap.
- Login usa usuario y contraseña propia, independiente de identidad Tailscale.
- Contraseñas usan Argon2id y nunca se almacenan reversiblemente.
- Passwords, sesiones y tokens de enrolamiento se almacenan como hashes; nunca se
  devuelven secretos completos a la UI salvo las respuestas efimeras documentadas de
  creacion y enrolamiento.
- El secreto de un token de enrolamiento solo se muestra en la respuesta de creacion,
  una vez, y la credencial de agente solo se entrega al agente durante el enrolamiento
  o replay exacto autorizado.
- Sesiones usan cookies `HttpOnly`, `Secure` y `SameSite` apropiado. La unica
  excepcion a `Secure` es el modo `development` limitado a loopback definido en
  [`docs/security-and-operations.md`](docs/security-and-operations.md).
- Recuperacion de contraseña se realiza mediante comando local, no email.
- Ninguna metrica, configuracion o dato historico es accesible sin autenticacion.
- Endpoints tecnicos publicos solo pueden revelar disponibilidad del proceso, nunca
  datos del sistema monitoreado.

### 4.8 Instalacion y operacion

Una release DEBE producir ejecutables Linux `amd64` y `arm64`. El frontend Vue se
compila antes y queda embebido en el ejecutable Go.

El mismo ejecutable PUEDE ofrecer modos y comandos como:

```text
atalaya server
atalaya agent
atalaya bootstrap local-agent
atalaya admin create
atalaya admin reset-password
atalaya migrate
atalaya backup
atalaya verify
atalaya version
```

El instalador DEBE:

- Detectar arquitectura soportada.
- Verificar checksum o firma del artefacto.
- Crear usuario, directorios y permisos restringidos.
- Generar configuracion y clave maestra cuando corresponda.
- Instalar y arrancar unidades `systemd` para rol server o agent. En Oracle, instalar
  el rol server configura tambien el agente local, su credencial y su unidad.
- Ser repetible sin destruir configuracion ni datos existentes.

Tailscale instalado, autenticado y conectado es una precondicion. HTTPS se publica
dentro de la tailnet mediante Tailscale Serve segun
[`docs/security-and-operations.md`](docs/security-and-operations.md).

## 5. Limitacion conocida de v1

Servidor, evaluador de alertas y agente inicial viven en Oracle. Si Oracle cae por
completo, ningun proceso local puede enviar una alerta.

v1 DEBE mostrar y documentar esta limitacion, pero NO DEBE depender de un servicio
externo. Una fase futura agregara un watchdog minimo en otro host propio siempre
encendido. Ese watchdog detectara ausencia del servidor y enviara notificacion sin
alojar dashboard ni historico.

## 6. Fuera de alcance v1

- Multiusuario, equipos, roles o permisos granulares.
- Hosts Windows o macOS.
- Exposicion directa a Internet.
- OAuth, SSO o dependencia exclusiva de autenticacion Tailscale.
- App movil nativa.
- Buffer de metricas en agentes durante caidas del servidor.
- Logs centralizados, tracing distribuido o reemplazo de una plataforma APM.
- Plugins ejecutables arbitrarios o scripts definidos desde UI.
- Checks ICMP, DNS, certificados TLS, contenido HTTP o transacciones sinteticas.
- Alta disponibilidad del servidor o replicacion de SQLite.
- Descubrimiento automatico de hosts o servicios de la tailnet.
- Watchdog externo a Oracle.
- Integraciones con infraestructura laboral.

## 7. Direccion posterior a v1

Posibles extensiones, no compromisos:

- Watchdog propio en segundo host.
- Nuevos adaptadores de notificacion como ntfy, email o Google Chat.
- Checks adicionales.
- Reglas de mantenimiento y silencios programados.
- Comparacion entre hosts y dashboards personalizables.
- Publicacion del proyecto y paquetes `.deb`/`.rpm`.
- Checks locales especificos, como presencia de una sesion tmux.

Estas extensiones deben usar contratos existentes cuando sea razonable, pero v1 no
debe construir infraestructura especulativa para ellas.

## 8. Criterios de aceptacion v1

v1 se considera completa cuando:

1. Una instalacion limpia en Linux compatible deja servidor operativo mediante
   `systemd` sin instalar runtime de desarrollo.
2. Un administrador puede crearse y recuperar contraseña localmente.
3. Usuario no autenticado no puede leer metricas, historico o configuracion.
4. Una instalacion server en Oracle crea automaticamente su agente local y Oracle
   aparece en dashboard con CPU, memoria, uptime y filesystems reales.
5. Segundo host puede enrolarse con token temporal y comenzar a ser consultado.
6. Detener un agente produce estado no disponible y alerta tras umbral configurado.
7. Checks HTTP y TCP pueden gestionarse desde UI, respetan la politica de red y
   conservan resultados programados, historico y latencia.
8. Graficos distinguen datos crudos, agregados y periodos sin muestras.
9. Reglas sostenidas transitan entre `inactive`, `pending` y `firing`, y cierran su
   incidente al resolverse, sin duplicar incidentes.
10. Discord recibe apertura y resolucion de un incidente; errores quedan registrados.
11. Secretos no aparecen en respuestas API, logs ni exportaciones de configuracion.
12. Reiniciar o actualizar servidor conserva configuracion, sesiones validas segun su
    expiracion, historico e incidentes.
13. Jobs de agregacion y retencion mantienen limites configurados.
14. UI funciona en escritorio y telefono sin ocultar estados criticos.
15. Backup y restauracion documentados recuperan una instancia funcional.

## 9. Documentos relacionados

- [Indice tecnico](docs/README.md)
- [Arquitectura](docs/architecture.md)
- [Modelo de dominio y datos](docs/domain-model.md)
- [API y protocolos](docs/api.md)
- [Seguridad y operacion](docs/security-and-operations.md)
- [Plan de implementacion](docs/implementation-plan.md)

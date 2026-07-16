# Seguridad y operacion

## 1. Modelo de amenazas

Se protege contra:

- Usuario no autenticado dentro de tailnet.
- Host de tailnet comprometido intentando leer metricas o suplantar agente.
- Robo de SQLite o backup sin clave maestra.
- CSRF y XSS contra sesion administrativa.
- SSRF mediante targets configurables de checks.
- Filtracion de webhooks, passwords o credenciales en logs y API.
- Instalador o artefacto descargado corrupto.

No se promete resistir:

- Root comprometido en servidor.
- Root comprometido en host agente para integridad de sus metricas.
- Administrador malicioso, porque existe uno solo.
- Compromiso simultaneo de base y clave maestra.
- Ataques publicos directos; aplicacion no debe exponerse a Internet.

## 2. Red y TLS

- Dashboard escucha en `127.0.0.1:8080` por default y Tailscale Serve publica HTTPS
  443.
- Agente escucha en `127.0.0.1:9474` y Tailscale Serve publica ese listener por HTTPS,
  en 9474 como default fijo de v1. Nunca usa `0.0.0.0` por default. El instalador
  verifica que la CLI pueda publicar simultaneamente HTTPS 443 y 9474 y falla mostrando
  version detectada, capacidad ausente y matriz de compatibilidad de la release si no
  puede; no elige otro puerto silenciosamente.
- El backend 9474 permanece en loopback y no se abre en firewall. ACLs/grants de la
  tailnet limitan quien alcanza el endpoint HTTPS publicado por Tailscale Serve.
- Servidor usa MagicDNS y valida TLS para agentes.
- No agregar opcion UI para ignorar TLS.
- `/healthz` revela solamente salud binaria.

Ejemplo conceptual, sujeto a CLI vigente de Tailscale:

```bash
tailscale serve --bg http://127.0.0.1:8080
```

Agente requiere una segunda regla Serve equivalente para su puerto HTTPS. Instalador
detecta version de CLI y comprueba rutas efectivas; no concatena comandos suponiendo
una sintaxis fija.

Cada release publica una matriz con la version estable probada y la version mas antigua
probada. Compatibilidad se decide por las capacidades observadas para configurar 443 y
9474 simultaneamente y confirmar ambas rutas; no existe un numero minimo global fuera
de esa matriz. El diagnostico siempre incluye la version detectada y la operacion que
fallo.

Instalador debe detectar Tailscale desconectado y fallar con instruccion clara, no
abrir listener publico como fallback.

## 3. Passwords y sesiones

- Argon2id con salt aleatoria y parametros guardados junto a hash.
- Parametros se calibran para servidor objetivo y tienen minimo seguro documentado en
  codigo.
- Comparacion y mensajes de login evitan enumeracion.
- Rate limit global y por username normalizado con backoff para login. Respuesta no
  revela si username existe.
- Token de sesion generado con CSPRNG, al menos 256 bits.
- DB guarda hash del token.
- Cookie `HttpOnly`, `Secure`, `SameSite=Strict` salvo necesidad probada, `Path=/`.
- Expiracion idle default 24 horas; absoluta default 7 dias.
- Estado mutante requiere token CSRF ligado a sesion.
- UI no inserta HTML no confiable; CSP restringe scripts a assets propios.

Servidor no usa `X-Forwarded-For` ni headers de identidad Tailscale para decisiones de
auth, autorizacion o rate limit. Conexion proxied se registra como tal sin atribuir IP
del usuario. Login valida `Origin`; identidad Tailscale reenviada puede registrarse como
dato informativo solo si conexion inmediata es loopback y modo Serve esta habilitado,
nunca autentica ni autoriza.

Auditoria persiste el `remote_addr` realmente observado y, por separado, identidad
proxy informativa nullable con su marca de confianza. No etiqueta loopback ni headers
reenviados como IP tailnet del administrador.

Produccion siempre exige origen HTTPS y cookie `Secure`. El modo explicito
`development` puede omitir `Secure` para el proxy HTTP de Vite solo cuando listener y
origen publico son loopback (`localhost`, `127.0.0.1` o `::1`). Startup rechaza esa
combinacion fuera de loopback; no existe un toggle de cookie insegura en configuracion
de produccion.

Headers minimos:

- `Content-Security-Policy`
- `X-Content-Type-Options: nosniff`
- `Referrer-Policy: no-referrer`
- `Permissions-Policy` restrictiva
- `Frame-Options` equivalente mediante CSP `frame-ancestors 'none'`
- `Cache-Control: no-store` en autenticacion, respuestas que establecen cookies y
  respuestas con secretos efimeros

## 4. Secretos

Clave maestra:

- 32 bytes aleatorios generados al instalar servidor.
- Archivo separado de SQLite, propiedad del usuario del servicio y modo `0600`.
- Nunca se genera automaticamente si DB existente requiere otra clave.
- Debe incluirse de forma protegida en backup; sin ella webhooks y credenciales de
  agentes no son recuperables.

Secretos en DB usan AES-256-GCM con nonce unico y associated data que incluya tipo e ID
de entidad. Registro guarda version de formato para futura rotacion.

Passwords y tokens de sesion/enrolamiento se hashean, no se cifran. Credenciales que
servidor necesita reutilizar, como webhook Discord y bearer de agente, se cifran.

El token de enrolamiento se suministra al agente mediante prompt interactivo o archivo
`0600` eliminado tras su uso. Nunca se pasa por argv ni queda en variables
persistentes, para no filtrarse en historial de shell, listado de procesos o logs del
instalador.

Logs aplican redaccion por nombre y tipo. URLs con userinfo o query sensible no se
registran completas. Errores de Discord no incluyen webhook URL.

## 5. SSRF y checks

Como administrador define targets, riesgo es menor pero no nulo: credenciales robadas
no deben convertir dashboard en proxy interno arbitrario.

- Solo esquemas `http` y `https` en checks HTTP.
- Rechazar userinfo en URL.
- Puertos entre 1 y 65535.
- Limitar redirects y volver a validar destino de cada redirect.
- Bloquear loopback, link-local, multicast, unspecified y metadata cloud por default.
- Permitir rango Tailscale `100.64.0.0/10` por default. Redes privadas RFC1918
  requieren CIDRs agregados desde settings autenticados. Direcciones publicas quedan
  bloqueadas por default.
- Resolver DNS y validar todas las IPs antes de conectar; mitigar DNS rebinding usando
  dialer que conecta IP validada.
- Limitar headers, cuerpo descartado, tiempo y concurrencia.

Oracle metadata (`169.254.169.254`) permanece bloqueada siempre en v1.
Loopback, link-local, multicast, unspecified y metadata no pueden habilitarse mediante
CIDR. Validacion al guardar revisa sintaxis; cada ejecucion y redirect vuelve a resolver
y exige que todas las IPs resultantes cumplan politica. Resultado mixto se rechaza.
La misma politica se aplica a TCP y a `agent_url`; un agente solo puede anunciar un
hostname MagicDNS o una direccion Tailscale con HTTPS valido.

La politica de red de checks no cubre el egress de notificaciones: ese egress se
restringe por proveedor. Cada adaptador valida la URL con un parser estructurado.
Discord exige esquema `https`, hostname ASCII normalizado exactamente `discord.com`,
puerto vacio o 443, ausencia de userinfo, query y fragment, y path con exactamente los
segmentos `/api/webhooks/{id}/{token}` no vacios. Se rechazan escapes o segmentos
adicionales al guardar y antes de cada entrega o test. Un `webhook_url` nunca puede
usarse como proxy hacia la red interna ni hacia destinos arbitrarios.

El adaptador Discord no sigue redirects y usa timeout HTTP total de 15 segundos, menor
que el lease de entrega de 60 segundos. El payload final se persiste sin secretos al
crear la entrega y no se vuelve a renderizar durante reintentos. Logs y errores no
incluyen URL, cuerpo ni headers sensibles.

## 6. Usuarios y permisos del sistema

Servidor y agente corren como usuarios dedicados sin shell. No corren como root tras
instalacion.

Paths recomendados:

```text
/usr/local/bin/atalaya
/etc/atalaya/server.yaml
/etc/atalaya/master.key
/etc/atalaya/agent.yaml
/var/lib/atalaya/atalaya.db
/var/lib/atalaya/agent-credential
```

- Config sin secreto puede ser `0640`; claves y credenciales `0600`.
- Directorios de datos no son accesibles por otros usuarios.
- Unidad `systemd` usa hardening compatible: `NoNewPrivileges`, `PrivateTmp`,
  `ProtectSystem`, `ProtectHome` y paths de escritura explicitos.
- Capacidades Linux adicionales requieren decision documentada. Recoleccion v1 no
  deberia necesitarlas.

## 7. Configuracion de arranque

Archivos config solo contienen valores necesarios antes de DB:

- Direccion de escucha.
- Path SQLite.
- Path de clave maestra.
- Nivel/formato de logs.
- URL publica esperada y confianza de proxy local Tailscale Serve.
- En agente: URL servidor, URL anunciada, path credencial y listener.

Hosts, checks, reglas, canales, intervalos y retencion cotidiana viven en DB y UI.
Variables de entorno pueden sobrescribir bootstrap para desarrollo, no deben ser unica
forma de configurar produccion.

## 8. Backup y restauracion

Backup consistente usa API de backup SQLite o `VACUUM INTO`; no copiar archivo DB vivo
ignorando WAL.

El backup de datos contiene:

- Snapshot SQLite consistente.
- Config de arranque sin secretos.
- Version de aplicacion y schema.

`atalaya backup --output <path>` produce un archivo de datos modo `0600` y nunca incluye
`master.key`. El backup por si solo no permite descifrar credenciales. La clave se
exporta explicitamente con `atalaya backup-key --output <separate-path>`, tambien
`0600`, despues de una confirmacion interactiva o flag no interactivo explicito. El
comando rechaza sobrescribir un archivo existente, y la documentacion exige custodiar
ambos artefactos en ubicaciones o mecanismos de cifrado distintos. v1 no
afirma cifrar esos artefactos: quien obtenga DB y clave puede leer secretos.

Procedimiento de restauracion:

1. Detener servidor.
2. Restaurar el backup de datos y una copia separada de la clave maestra con
   propietario/permisos correctos.
3. Ejecutar verificacion de integridad SQLite.
4. Instalar una version compatible con `--no-start` y ejecutar `atalaya migrate`.
5. Ejecutar `atalaya verify`; cualquier fallo detiene la restauracion.
6. Iniciar y comprobar `/healthz`, login, hosts y descifrado de canales.

Antes de cada migracion de upgrade, crear backup automatico local de datos. La clave
existente no se copia porque v1 no la rota durante migraciones. Retencion de backups
debe ser acotada y configurable. Copias separadas de datos y clave fuera de Oracle son
recomendadas, pero su automatizacion queda fuera de v1.

## 9. Instalacion y releases

Release incluye:

- Ejecutables `linux/amd64` y `linux/arm64`.
- Checksums SHA-256 y, cuando infraestructura lo permita, firma.
- Script de instalacion versionado.
- Notas con migraciones y compatibilidad de agente.

Instalador:

- Acepta `server`, `agent` y `upgrade`. El rol `server` en Oracle instala tambien el
  agente local y lo registra sin token de UI.
- Acepta `--no-start` para restore y mantenimiento offline; en ese modo instala
  artefactos y units, pero no inicia ni reinicia servicios.
- Descarga por HTTPS y verifica checksum antes de reemplazar binario.
- Usa archivos temporales y rename atomico.
- Es idempotente y no sobrescribe secretos existentes.
- Conserva binario, DB, clave y config previos como unidad de rollback hasta health
  check y verificacion local exitosos. `atalaya verify` debe abrir config/DB, comprobar
  version de schema, ejecutar lectura y descifrar todo secreto o sentinel de cifrado.
  Si hubo migracion, rollback restaura snapshot pre-migracion junto con binario
  anterior; nunca ejecuta binario viejo sobre schema nuevo salvo compatibilidad
  declarada por esa release.
- No instala Tailscale silenciosamente.
- No ejecuta migracion destructiva sin backup.

`atalaya verify` comprueba config, schema, acceso de lectura y descifrado de secretos.
La restauracion es un procedimiento documentado: detener servicio, restaurar DB, clave
y config, comprobar integridad, ejecutar `atalaya migrate`, ejecutar `atalaya verify` y
solo entonces arrancar una version compatible.

Comando `curl | sh` puede ofrecer conveniencia, pero documentacion tambien debe mostrar
descarga, inspeccion y ejecucion separadas.

## 10. Logs y diagnostico

- Logs estructurados a stdout/stderr; `journald` gestiona persistencia.
- Cada request recibe `request_id`.
- Niveles: info para cambios/operacion, warn para fallos recuperables, error para
  trabajo perdido o invariantes.
- No registrar cada pull exitoso en info indefinidamente; usar debug o resumen.
- Registrar cambios administrativos sin valores secretos.
- Endpoint autenticado de estado muestra jobs atrasados y version, no logs completos.
- Rotacion/limites pertenecen a `journald` y se documentan en instalacion.

## 11. Disponibilidad y watchdog

v1 no puede avisar caida total de Oracle. No fingir cobertura mediante job dentro del
mismo servidor.

Fase futura instala watchdog minimo en segundo host propio:

- Consulta endpoint autenticado o heartbeat de servidor.
- Mantiene estado y cooldown local pequenos.
- Envia Discord directamente con secreto propio.
- No accede a SQLite ni aloja dashboard.
- Alerta tanto caida como recuperacion.

Hasta entonces, UI y documentacion de instalacion muestran limitacion.

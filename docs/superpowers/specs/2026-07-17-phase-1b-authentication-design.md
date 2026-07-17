# Diseno Fase 1B: autenticacion del servidor

Estado: aprobado para implementacion el 2026-07-17.

## Resultado

Fase 1B agrega autenticacion local completa sin ampliar v1: sesiones persistentes
hasheadas, CSRF ligado al secreto de sesion, login/logout/cambio de password, reset
local, rate limit determinista y proteccion de API/SPA. El servidor puede arrancar sin
administrador para permitir bootstrap local posterior.

## Limites y componentes

- `internal/auth` contiene reglas, use cases, reloj y aleatoriedad inyectables.
- `internal/database` implementa transacciones SQLite y migracion inmutable `002`.
- `internal/httpapi` adapta contratos JSON, cookie, middleware y fallback SPA.
- `internal/cli` ofrece `admin reset-password`; passwords solo entran por TTY sin eco.
- No hay bearer tokens, CORS, CSRF persistido, identidad Tailscale como autenticacion,
  estado de sesion en navegador ni dependencias nuevas.

## Sesion y concurrencia

El token contiene 32 bytes CSPRNG y usa base64url sin padding. SQLite guarda solo su
SHA-256 BLOB de 32 bytes. Idle dura 24 horas y expiracion absoluta 7 dias; `now >=`
cualquiera invalida. Validar y extender idle ocurre en una transaccion, limitado por
la expiracion absoluta. Crear sesion compara version actual del administrador para no
ganar una carrera contra reset/cambio.

Cambio de password verifica password actual y, en una transaccion, actualiza PHC,
timestamps/version, revoca todas las sesiones e inserta reemplazo. Reset local hace la
misma actualizacion y revocacion, sin reemplazo. Las sesiones sobreviven reinicios.

CSRF es `base64url(HMAC-SHA256(raw_session_token,
"atalaya\x00csrf\x00v1"))`; no se almacena. Logout y cambio exigen
`X-CSRF-Token`.

## HTTP y seguridad

`POST login`, `GET me`, `GET csrf`, `POST logout` y `POST change-password` viven bajo
`/api/v1/auth`. JSON es estricto, `application/json`, maximo 4 KiB. Login exige
`Origin == public_url`, rechaza `Sec-Fetch-Site: cross-site`, no enumera usuarios y
ejecuta Argon2id dummy para usuario desconocido.

Cookie `atalaya_session`: `HttpOnly`, `SameSite=Strict`, `Path=/`, sin `Domain`,
expira con limite absoluto y usa `Secure`, excepto development HTTP loopback. Toda API
usa `no-store`. Request IDs propios de 128 bits ignoran entrada. CSP, nosniff,
referrer y permissions policy se aplican globalmente; errores son JSON redactado.

Rate limit en memoria: bucket global 30/minuto con burst 10; por username normalizado,
el quinto fallo bloquea 1 segundo y duplica hasta 60. Exito limpia entrada; maximo 1024
entradas y purga a los 15 minutos. Reiniciar proceso reinicia limiter.

`trusted_proxy` acepta `none` o `tailscale_serve` y default `none`.
`tailscale_serve` solo es valido en production con HTTPS y listener loopback. Headers
`Tailscale-User-Login/Name` se decodifican RFC 2047 y registran como informacion solo
si peer inmediato es loopback. Nunca participan en auth, autorizacion o rate limit;
`X-Forwarded-For` se ignora.

## Rutas y validacion

Assets, `/login` y `/healthz` son publicos. Otro GET/HEAD UI seguro requiere sesion y
redirige a `/login`. `/api`, traversal, assets ausentes y paths con apariencia de
archivo nunca reciben HTML. Pruebas cubren migracion/corrupcion, expiracion,
revocacion, carreras, reinicio, CSRF, limiter, cookie, origen, proxy, headers, fallback
y CLI.

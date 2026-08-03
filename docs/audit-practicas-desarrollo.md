# Auditoría de buenas prácticas de desarrollo

Fecha: 2026-08-03. Alcance: backend Go (`cmd/`, `internal/`), frontend Vue/TS
(`web/`), CI/CD (`.github/workflows/`) y gobernanza del repositorio. Este
documento es una nota de mantenedores, no normativa; no participa en la
[precedencia de documentos públicos](README.md#precedencia).

## Fortalezas

- **CI/CD**: GitHub Actions con acciones fijadas por SHA (no tags mutables),
  `permissions: contents: read` por defecto, `concurrency` para cancelar runs
  duplicados, y gates de lint/test/e2e/cross-build en cada pull request.
- **CodeQL** activo para Go y JavaScript/TypeScript, con schedule semanal
  además de push/PR.
- **Dependabot** configurado para `gomod`, `npm` y `github-actions`, agrupando
  actualizaciones minor/patch.
- **Conventional Commits** + `release-please`: changelog automático y canales
  `stable`/`dev` separados.
- **Seguridad de autenticación**: rate limiter propio, protección CSRF,
  sesiones con expiración idle y absoluta, comparación en tiempo constante
  (`crypto/subtle`) para tokens, hashing de contraseñas.
- **Acceso a datos**: 100% de las queries SQL parametrizadas (`?`); no se
  encontró construcción de SQL vía `Sprintf` o concatenación.
- **Optimistic locking** (`version`) en `administrators`, `settings` y
  `sessions`, evita condiciones de carrera en escrituras concurrentes.
- **Frontend**: TypeScript estricto (`strict: true`), ESLint + Prettier +
  `type-check` como parte de `make lint`.
- `npm audit --omit=dev`: 0 vulnerabilidades en dependencias de producción.
- Documentación técnica extensa y sincronizada (`spec.md`,
  `docs/architecture.md`, `docs/api.md`, `docs/domain-model.md`,
  `docs/security-and-operations.md`, `docs/dependencies.md`).
- Sin `TODO`/`FIXME`/`HACK` sueltos en el código. Sin uso de `io/ioutil`
  (deprecado).

## Gaps identificados

| Área | Hallazgo | Severidad |
| --- | --- | --- |
| ~~Lint Go~~ | ~~Solo `go vet` + `gofmt` en `make lint`.~~ **Resuelto** (2026-08-03): `golangci-lint` agregado, ver [Resolución: golangci-lint](#resolución-golangci-lint). | — |
| Cobertura | `internal/notifications` 33%, `internal/httpapi` 41%, `internal/checks` 46% de cobertura — paquetes con lógica de negocio y superficie HTTP por debajo del 50%. | Media |
| Cobertura | `internal/retention/worker.go` (52 líneas, lógica real de purga de datos) sin archivos de test, 0% de cobertura. | Media |
| Cobertura | No existe umbral mínimo de cobertura exigido en CI (`go test -cover` se ejecuta pero no se valida contra un mínimo). | Baja |
| Manejo de errores | 34 errores ignorados con `_ =` en código no-test, mayoría `rows.Close()` (aceptable) pero sin revisión sistemática documentada. | Baja |
| Protección de rama | No verificable desde el repositorio local; confirmar en GitHub que `main` exige review y status checks (CODEOWNERS sugiere que se espera). | Info |
| ~~SAST adicional~~ | ~~No corre `gosec`.~~ **Resuelto** (2026-08-03): incluido en `golangci-lint`. | — |

## Recomendación

Proyecto en buen estado, por encima del estándar típico de un repositorio
mono-mantenedor. Si se busca subir un escalón, el mayor retorno está en:

1. ~~Agregar `golangci-lint`...~~ **Resuelto** (2026-08-03).
2. Cubrir con tests `internal/retention` — única lógica de negocio real sin
   cobertura, y toca borrado de datos (zona sensible). Sigue pendiente.

## Resolución: golangci-lint

`golangci-lint` v2 se agregó como dependencia de herramienta pinneada en
`go.mod`/`go.sum` (mecanismo `go tool`, sin instalación aparte en CI) y se
integró en `make lint`. Config en `.golangci.yml`: set default
(`errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`) más `gosec`.

Al correrlo por primera vez aparecieron 376 hallazgos. Triage:

- **319 errcheck**: la enorme mayoría eran `defer rows.Close()`,
  `defer tx.Rollback()`, `fmt.Fprint*` a streams de CLI, y cierres de
  archivos/conexiones en rutas de limpieza tras error — patrones donde
  chequear el error no cambia el comportamiento del programa (se verificó
  código por código, no se asumió). Se resolvieron con
  `errcheck.exclude-functions` en `.golangci.yml` (lista explícita y
  acotada por función, no un exclude global) más una regla que excluye
  `errcheck`/`gosec` en archivos `_test.go`.
- **46 gosec**: todos los casos en código de producción resultaron falsos
  positivos tras revisión manual (cláusulas SQL `IN (...)` con
  placeholders, no datos concatenados; `exec.Command` con path de binario
  controlado por config del operador; cookie de sesión con
  `HttpOnly`/`Secure`/`SameSite` ya seteados; conversiones de enteros con
  bounds-check previo; `math/rand` usado solo para jitter de scheduling,
  nunca para tokens). Se documentaron con `//nolint:gosec` puntual y razón
  en cada línea. `G104` (duplicaba lo que ya cubre errcheck) se
  deshabilitó en la config de gosec.
- **7 staticcheck**: reales, corregidos (De Morgan, switch en vez de
  if/else-if, declaración+asignación fusionada, string de error en
  minúscula).
- **4 unused**: código muerto real, eliminado (`runServerAndCollection`,
  `utcTimestamp`, `errOrCommit`, `httpsPort`).

`make lint` y `make test` quedan en verde con `golangci-lint run` incluido.

## Arquitectura y SOLID

Revisión enfocada en separación de capas, acoplamiento e inversión de
dependencias, considerando el alcance real del proyecto (uso personal, un
solo desarrollador, SQLite, un administrador).

### Fortalezas

- **Dependency Inversion aplicado correctamente**: cada dominio (`auth`,
  `checks`, `alerts`, `hosts`, `metrics`, `collection`, `notifications`,
  `settings`, `hostquery`) define su propia interfaz `Repository` en el
  paquete de dominio. `internal/database` implementa esas interfaces; los
  paquetes de dominio nunca importan `database`, y `database` nunca importa
  `httpapi` ni `cli`. No es habitual ver esta disciplina en un proyecto
  solo-dev.
- **Interface Segregation** del lado consumidor: las interfaces son angostas
  y específicas por caso de uso (`checks.SchedulerRepository`,
  `checks.ManualRepository`, `metrics.RetentionRepository`, etc.) en vez de
  una interfaz `Repository` monolítica por dominio.

### Problemas encontrados (menores dado el alcance)

1. **`Database` como implementador único de 15 interfaces** — un solo
   struct (`internal/database/database.go`) implementa todas las interfaces
   de todos los dominios. Cada interfaz es angosta (ISP respetado del lado
   consumidor), pero el implementador concentra todo el acceso a datos. Para
   SQLite single-file con un solo dev es un tradeoff razonable (una
   conexión, un lugar para migraciones). Severidad: baja. Acción: ninguna
   por ahora; revisar solo si el proyecto crece en dominios.
2. **`NewAuthenticated` usa type-assertion para descubrir dependencias en
   runtime** (`internal/httpapi/auth_handler.go`, ~161 líneas). Recibe
   `hostRepositories ...hostquery.Repository` (variádico de 0 o 1) y después
   prueba en runtime si ese valor también implementa `metrics.Repository`,
   `checks.Repository`, `alerts.Repository`, `notifications.Repository`,
   `settingsdomain.Repository`, `system.Provider` (8 type assertions
   seguidas). Esto esconde las dependencias reales de `authAPI` — no son
   visibles en la firma de la función, se descubren en runtime. La función
   además mezcla defaulting de config, wiring de capacidades opcionales y
   registro de rutas. Rompe el principio de dependencias explícitas.
   Severidad: media. Fix de bajo costo: reemplazar por parámetros u opciones
   funcionales explícitas (`WithMetrics(repo)`, `WithChecks(repo)`, etc.).
3. **`internal/cli/application.go` concentra 1078 líneas** con el dispatch
   de 12 subcomandos no relacionados (`server`, `agent`, `backup`,
   `migrate`, `admin`, `tailscale`, `bootstrap`). No es un problema de
   diseño (mismo paquete, mismo comportamiento), es organización de
   archivos — SRP violado a nivel archivo, no a nivel arquitectura.
   Severidad: cosmética. Fix trivial: separar por familia de comando
   (`cli/server.go`, `cli/admin.go`, ...) dentro del mismo paquete.
4. **Frontend: vistas con demasiada lógica embebida** —
   `AlertsView.vue` (786 líneas, 275 de script), `CheckDetailView.vue`
   (668), `HostManagementView.vue` (608) mezclan fetch, validación de
   formularios, estado y presentación en el propio componente. Solo existe
   un composable extraído (`useHostPolling`) en todo `web/src`. Esto explica
   en parte la cobertura de tests baja en el frontend: las vistas con más
   lógica de negocio son las menos aisladas para testear. Severidad: baja
   para el tamaño actual; sube si el frontend crece. Acción sugerida: al
   tocar una vista grande, extraer su lógica a composable de paso, no como
   refactor dedicado.

### Fuera de alcance a propósito

No se recomienda introducir arquitectura hexagonal, contenedor de DI, event
bus ni CQRS. El proyecto ya logra la separación dominio/infraestructura que
importa sin ese aparato adicional; agregarlo para un caso de uso de "un solo
dev, SQLite, un administrador" sería sobre-ingeniería.

### Prioridad si se actúa

De los cuatro puntos, el de mayor relación costo/beneficio es el **punto 2**
(wiring por type-assertion en `NewAuthenticated`), porque afecta
directamente la legibilidad de qué depende de qué — el objetivo central que
SOLID intenta proteger.

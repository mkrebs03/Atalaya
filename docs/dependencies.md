# Dependencias directas de runtime y fundacionales

Este registro cubre dependencias de runtime o fundacionales fijadas directamente. Las
versiones transitivas reproducibles viven en `go.sum` y `web/package-lock.json`; antes
de distribuir una release se genera el aviso de terceros completo desde esos locks.

| Dependencia | Version | Uso | Licencia upstream |
| --- | --- | --- | --- |
| `modernc.org/sqlite` | `v1.53.0` | Driver SQLite pure Go | BSD-3-Clause |
| `golang.org/x/crypto` | `v0.54.0` | Argon2id | BSD-3-Clause |
| `golang.org/x/sys` | `v0.47.0` | Clave maestra y lectura/restauracion segura del terminal en Linux | BSD-3-Clause |
| `golang.org/x/term` | `v0.45.0` | Deteccion de la terminal interactiva para passwords | BSD-3-Clause |
| `go.yaml.in/yaml/v3` | `v3.0.4` | Configuracion YAML estricta | MIT y Apache-2.0, segun archivo |
| `vue` | `3.5.40` | Runtime de UI | MIT |
| `vue-router` | `5.2.0` | Navegacion de UI | MIT |

SQLite, Argon2id y YAML respaldan la persistencia, los hashes de password y la
configuracion estricta de Fase 1A. La prueba de compatibilidad fundacional continua
abriendo SQLite en memoria, derivando una clave Argon2id y verificando con
`CGO_ENABLED=0` que el decoder YAML con `KnownFields(true)` rechace campos
desconocidos. `x/sys` respalda el cargador Linux de clave maestra y la restauracion
segura del terminal; `x/term` detecta la terminal local usada por el bootstrap de
passwords. El build compila las pruebas para Linux `amd64` y `arm64` sin ejecutarlas de
forma cruzada ni conservar binarios de prueba como entregables.

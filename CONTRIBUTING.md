# Contribuir a Atalaya

Gracias por tu interés en Atalaya. Antes de cambiar comportamiento, revisa la
[especificación del producto](spec.md) y la [documentación técnica](docs/README.md).
Los cambios que alteren el contrato deben actualizar también la documentación y las
pruebas correspondientes.

## Entorno

Las versiones de Go, Node.js y npm están fijadas en [`.tool-versions`](.tool-versions)
y en los archivos de frontend. Se requiere GNU Make; Chromium solo es necesario para
las pruebas E2E.

## Verificación

Ejecuta antes de abrir un pull request:

```bash
make lint
make test
make build
make install-e2e-browser
make test-e2e
```

Las pull requests ejecutan la validación completa (`lint`, tests, E2E y builds). Tras
un merge, `main` ejecuta una verificación de build más breve, CodeQL vuelve a analizar
la rama principal y el CI exitoso permite publicar un nuevo build del canal `dev`.

Los cambios de UI deben incluir pruebas relevantes y, cuando corresponda, actualizar
los snapshots visuales. Los cambios de seguridad deben cubrir sesiones, CSRF,
enrolamiento, almacenamiento de credenciales, migraciones o validación SSRF según el
área afectada.

## Estilo de cambios

- Go idiomático, paquetes pequeños e interfaces explícitas en las fronteras.
- Vue 3 Composition API, `<script setup>` y TypeScript estricto.
- Nombres ligados al dominio: `host`, `snapshot`, `check`, `incident`, `enrollment` y
  `notification`.
- No incluir secretos, credenciales, webhooks reales, datos de producción ni artefactos
  de build en commits.
- Usar mensajes de commit concisos con formato Conventional Commits.

Los pull requests deben explicar el alcance, listar los comandos de verificación y
adjuntar capturas cuando modifiquen la interfaz.

## Releases

Atalaya usa dos canales:

- `stable`: una Release PR aprobada calcula la siguiente versión SemVer, actualiza
  `CHANGELOG.md` y publica una release inmutable para usuarios finales.
- `dev`: cada CI exitoso sobre `main` publica una pre-release inmutable con un tag
  histórico `dev-<run>-<short-sha>`. El instalador resuelve el tag más reciente para
  probar el último estado integrado; este canal no debe usarse en producción.

Los mensajes Conventional Commits determinan el siguiente incremento: `fix` aumenta
patch, `feat` aumenta minor y un cambio incompatible llega a `1.0.0`. Una excepción
explícita puede incluir el footer `Release-As: x.y.z` en el commit que debe iniciar la
Release PR. La primera release se inicializa como `0.0.1` mediante ese mecanismo.

El workflow necesita el secret de Actions `ATALAYA_RELEASE_TOKEN`, con permisos para
crear PRs, tags, releases y actualizar `CHANGELOG.md`. Debe ser un token de
automatización dedicado y con alcance mínimo al repositorio. La opción de permitir que
`GITHUB_TOKEN` apruebe pull requests debe permanecer desactivada; el workflow no debe
aprobar sus propios cambios.

Para inicializar la primera release, agrega una sola vez el footer `Release-As: 0.0.1`
en un commit de `main`. Release Please abrirá la Release PR correspondiente; al
aprobarla y fusionarla se generarán el tag `v0.0.1` y sus artefactos estables.

Para instalar el estado dev más reciente:

```bash
curl -fsSL https://raw.githubusercontent.com/mkrebs03/Atalaya/main/scripts/install.sh \
  | sudo sh -s -- server --channel dev
```

Para actualizar una instalación existente al estado dev más reciente:

```bash
curl -fsSL https://raw.githubusercontent.com/mkrebs03/Atalaya/main/scripts/install.sh \
  | sudo sh -s -- upgrade --channel dev
```

`--channel dev` consulta las releases públicas, selecciona el `dev-*` más reciente y
verifica sus checksums. No reutilices tags `vX.Y.Z` ni edites manualmente una release
estable; los tags `dev-*` también son históricos e inmutables.

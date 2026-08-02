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

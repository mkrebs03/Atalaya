# Frontend de Atalaya

Aplicacion Vue 3 de Atalaya. Usa TypeScript estricto, Composition API, Vue Router,
Vitest, Playwright, ESLint y Prettier. No forma parte del runtime como proceso: Vite
genera `dist/` y [`assets.go`](assets.go) lo embebe en el ejecutable Go.

Los comandos soportados se ejecutan desde la raiz con `make lint`, `make test`,
`make build` y `make test-e2e`. Para desarrollo interactivo usa `make dev-server` y
`make web-dev` en terminales separadas. La documentacion completa esta en el
[`README` raiz](../README.md).

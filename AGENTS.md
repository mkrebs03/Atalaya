# Repository Guidelines

## Project Structure & Module Organization

Atalaya is preparing its first public release. Treat `spec.md` as the product
contract and `docs/README.md` as the technical document index and precedence list.
Current technical docs live under `docs/`.

Implementation structure is:

- `cmd/atalaya/`: Go CLI entrypoint.
- `internal/`: Go modules added as vertical slices require them.
- `web/`: Vue 3, TypeScript, and Vite frontend.
- `scripts/`: installation, release, and operational helper scripts.
- `docs/`: architecture, API, domain model, security, and release roadmap.

Do not create empty packages ahead of use; add modules when a vertical slice needs
them.

## Build, Test, and Development Commands

The repository must keep these stable commands:

- `make test`: run Go and frontend tests.
- `make lint`: run formatting, linting, and static checks.
- `make build`: build the Linux `amd64`/`arm64` deliverables.
- `make test-e2e`: run end-to-end checks once UI workflows exist.

Keep command behavior and references aligned with `docs/roadmap.md` and
the root `README.md`.

## Coding Style & Naming Conventions

Use idiomatic Go with small packages and explicit interfaces at module boundaries.
Prefer the Go standard library unless a dependency demonstrably reduces risk or
complexity. Use `github.com/mkrebs03/atalaya` as the Go module/import prefix.
Frontend code should use Vue 3 Composition API, `<script setup>`,
strict TypeScript, Vue Router, and local CSS without a visual framework in v1.

Use descriptive names tied to domain terms: `host`, `snapshot`, `check`, `incident`,
`enrollment`, and `notification`. Keep secrets out of logs, API responses, fixtures,
and browser storage.

## Testing Guidelines

Add behavior-focused tests with each implemented slice. Cover security-sensitive
paths such as sessions, CSRF, enrollment tokens, credential storage, migrations, and
SSRF validation. Use deterministic clocks for scheduler, retention, and alert tests.
Historical series tests must distinguish missing data from zero values.

## Commit & Pull Request Guidelines

Git history currently uses Conventional Commits, for example
`docs: define Atalaya v1 specification`. Continue using concise messages like
`feat(agent): collect filesystem metrics` or `test(auth): cover csrf expiry`.

Pull requests should describe scope, list verification commands, link issues when
available, and include screenshots for UI changes. Contract changes must update the
relevant document and tests in the same change.

## Agent-Specific Instructions

Before implementing behavior, read `spec.md` and the relevant document under
`docs/`. Follow the precedence order in `docs/README.md`; if requirements conflict,
stop and update documentation through an explicit decision rather than choosing
silently.

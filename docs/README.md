# Documentación técnica

La documentación define el contrato de Atalaya y complementa el
[README principal](../README.md). Los documentos de producto y operación son la
referencia para utilizar una release; las notas de diseño histórico sirven para
mantenedores y no sustituyen el contrato vigente.

## Documentos públicos

- [Especificación del producto](../spec.md): visión, alcance, seguridad funcional,
  limitaciones y criterios de aceptación.
- [Arquitectura](architecture.md): stack, topología, componentes y flujos.
- [API y protocolos](api.md): endpoints, autenticación, enrolamiento y contratos de
  comunicación.
- [Modelo de dominio y datos](domain-model.md): entidades, estados, scheduling y
  persistencia.
- [Seguridad y operación](security-and-operations.md): amenazas, red, secretos,
  instalación, systemd, backup, restore y actualización.
- [Dependencias](dependencies.md): dependencias directas, toolchain y licencias.
- [Roadmap](roadmap.md): alcance de la primera release y trabajo posterior.
- [Licencia del proyecto](../LICENSE): Apache License 2.0.

## Precedencia

Cuando dos documentos discrepan, se aplica este orden:

1. `spec.md` para comportamiento visible, alcance y criterios de aceptación.
2. `docs/security-and-operations.md` para controles de seguridad y operación.
3. `docs/api.md` para contratos entre procesos y frontend.
4. `docs/domain-model.md` para semántica y persistencia.
5. `docs/architecture.md` para organización interna y stack.
6. `docs/roadmap.md` para secuencia de trabajo y prioridades futuras.

## Principios de mantenimiento

- No cambiar un requisito normativo sin actualizar `spec.md` y sus pruebas.
- No exponer secretos en API, logs, errores, fixtures o documentación.
- Mantener Linux `amd64` y `arm64` como arquitecturas de release.
- Preferir cambios pequeños, pruebas orientadas a comportamiento y migraciones
  compatibles hacia adelante.

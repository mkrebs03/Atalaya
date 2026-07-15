# Documentacion tecnica

## Indice

- [Arquitectura](architecture.md): stack, componentes, flujos y limites.
- [Modelo de dominio y datos](domain-model.md): entidades, estados, persistencia y
  retencion.
- [API y protocolos](api.md): contratos HTTP entre UI, servidor y agentes.
- [Seguridad y operacion](security-and-operations.md): amenazas, secretos, despliegue,
  backup y actualizacion.
- [Plan de implementacion](implementation-plan.md): fases, dependencias y condiciones
  de salida.

## Autoridad

Orden de precedencia ante contradicciones:

1. `spec.md` para comportamiento visible, alcance y criterios de aceptacion.
2. `docs/security-and-operations.md` para controles de seguridad.
3. `docs/api.md` para contratos entre procesos y frontend.
4. `docs/domain-model.md` para semantica y persistencia.
5. `docs/architecture.md` para organizacion interna y stack.
6. `docs/implementation-plan.md` para secuencia de trabajo, no comportamiento.

Si dos requisitos del mismo nivel se contradicen, detener implementacion y actualizar
documentacion mediante una decision explicita. No elegir silenciosamente.

## Reglas para agentes

- Implementar solamente alcance de fase o tarea asignada.
- No agregar servicios de infraestructura sin modificar primero arquitectura.
- No cambiar defaults normativos sin actualizar `spec.md`.
- No exponer secretos en API, logs, errores o fixtures.
- Escribir migraciones compatibles hacia adelante; nunca editar una migracion ya
  publicada.
- Agregar pruebas sobre comportamiento, no detalles internos accidentales.
- Mantener Linux `amd64` y `arm64` como plataformas soportadas.
- Preferir biblioteca estandar de Go. Agregar dependencia cuando reduzca riesgo o
  complejidad total de forma demostrable.
- Mantener frontend accesible, responsive y consistente con estados de dominio.
- Registrar decisiones nuevas en documento correspondiente antes o junto al codigo.

## Estado de decisiones

Decisiones cerradas:

- Go para servidor, agente y CLI.
- Vue 3, TypeScript y Vite para frontend.
- SQLite como unica base de datos v1.
- `systemd` para procesos Linux.
- Tailscale como red privada y Tailscale Serve para HTTPS del dashboard.
- Pull de metricas, intervalo por defecto de 60 segundos.
- Usuario administrador unico con auth local.
- Discord como primer adaptador de notificacion.

No decidido porque no bloquea inicio:

- Nombre publico definitivo y dominio de paquetes Go.
- Proveedor de releases mientras proyecto siga privado.
- Proveedores de notificacion posteriores a Discord.

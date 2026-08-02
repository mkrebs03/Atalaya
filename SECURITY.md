# Política de seguridad

Atalaya está diseñado para operar dentro de una tailnet privada. No expongas el
servidor directamente a Internet ni incluyas contraseñas, tokens, webhooks, claves
maestras o datos de producción en issues, logs o pull requests.

## Reportar una vulnerabilidad

No publiques una vulnerabilidad explotable en un issue público. Si el repositorio tiene
habilitados GitHub Security Advisories, utiliza la opción **Report a vulnerability**.
Si no está disponible, contacta privadamente a los mantenedores desde el perfil de
GitHub del proyecto e incluye únicamente la información necesaria para reproducir el
problema.

No envíes secretos reales ni credenciales de instalaciones activas. Para reportes
urgentes, indica el impacto, las versiones afectadas y una mitigación temporal si la
conoces.

## Alcance de seguridad

Las garantías y limitaciones conocidas están documentadas en
[`docs/security-and-operations.md`](docs/security-and-operations.md). El servidor
requiere Tailscale, autenticación propia y permisos restringidos para sus archivos de
estado y claves.

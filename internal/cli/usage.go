package cli

import (
	"fmt"
	"io"
)

func writeRootUsage(output io.Writer) {
	fmt.Fprint(output, `Usage: atalaya <command> [options]

Commands:
  server   Start the Atalaya HTTP server
  migrate  Apply database migrations without starting HTTP
  admin    Manage the local administrator
  agent    Start the Atalaya monitoring agent
  bootstrap  Bootstrap local Atalaya resources
  backup   Create a consistent data backup
  backup-key Export the master key separately
  verify   Verify configuration, schema, and encrypted secrets
  tailscale Configure and verify private Serve routes
  version  Print the Atalaya version
  help     Show command help
`)
}

func writeBootstrapUsage(output io.Writer) {
	fmt.Fprint(output, "Usage: atalaya bootstrap <command> [options]\n\nCommands:\n  local-agent  Bootstrap the local agent\n")
}

func writeBootstrapLocalAgentUsage(output io.Writer) {
	fmt.Fprint(output, `Usage: atalaya bootstrap local-agent --server-config <path> --agent-config <path> --agent-url <url> [options]

Required:
  --server-config <path>   Server YAML configuration
  --agent-config <path>    Agent YAML configuration to create or verify
  --agent-url <url>        Advertised https://<fqdn>.ts.net:9474 URL

Options:
  --display-name <name>    Host display name (default local-agent)
  --listen <addr>          Agent listener (default 127.0.0.1:9474)
  --credential-path <path> Credential file (default beside agent config)
`)
}

func writeAgentUsage(output io.Writer) {
	fmt.Fprint(output, "Usage: atalaya agent <command> [options]\n\nCommands:\n  enroll  Enroll this agent with the server\n\nStart: atalaya agent --config <path>\n")
}

func writeAgentEnrollUsage(output io.Writer) {
	fmt.Fprint(output, "Usage: atalaya agent enroll --config <path> [options]\n\nOptions:\n  --config <path>       Agent YAML configuration (required)\n  --display-name <name> Host display name (default local hostname)\n  --token-file <path>   0600 token file (default hidden prompt)\n")
}

func writeServerUsage(output io.Writer) {
	fmt.Fprintf(output, `Usage: atalaya server [--config <path>]

Options:
  --config <path>  Server YAML configuration (default %s)
`, defaultServerConfigPath)
}

func writeMigrateUsage(output io.Writer) {
	fmt.Fprintf(output, `Usage: atalaya migrate [--config <path>]

Options:
  --config <path>  Server YAML configuration (default %s)
`, defaultServerConfigPath)
}

func writeAdminUsage(output io.Writer) {
	fmt.Fprint(output, `Usage: atalaya admin <command> [options]

Commands:
  create  Create the single local administrator
  reset-password  Reset password and revoke all sessions
`)
}

func writeAdminResetPasswordUsage(output io.Writer) {
	fmt.Fprintf(output, "Usage: atalaya admin reset-password [--config <path>]\n\nOptions:\n  --config <path>  Server YAML configuration (default %s)\n", defaultServerConfigPath)
}

func writeAdminCreateUsage(output io.Writer) {
	fmt.Fprintf(output, `Usage: atalaya admin create [--config <path>] [--username <name>]

Options:
  --config <path>    Server YAML configuration (default %s)
  --username <name>  Administrator username (default admin)
`, defaultServerConfigPath)
}

func writeBackupUsage(output io.Writer) {
	fmt.Fprintf(output, "Usage: atalaya backup [--config <path>] --output <path>\n\nOptions:\n  --config <path>  Server YAML configuration (default %s)\n  --output <path>  New 0600 .tar.gz data backup (must not exist)\n", defaultServerConfigPath)
}

func writeBackupKeyUsage(output io.Writer) {
	fmt.Fprintf(output, "Usage: atalaya backup-key [--config <path>] --output <path> [--yes]\n\nOptions:\n  --config <path>  Server YAML configuration (default %s)\n  --output <path>  New 0600 raw master-key export (must not exist)\n  --yes            Confirm export without an interactive prompt\n", defaultServerConfigPath)
}

func writeVerifyUsage(output io.Writer) {
	fmt.Fprintf(output, "Usage: atalaya verify [--config <path>]\n\nOptions:\n  --config <path>  Server YAML configuration (default %s)\n", defaultServerConfigPath)
}

func writeTailscaleUsage(output io.Writer) {
	fmt.Fprint(output, `Usage: atalaya tailscale serve [options]

Options:
  --role <both|server|agent>  Routes to configure (default both)
  --server-config <path>      Server YAML when role is server
  --agent-config <path>       Agent YAML when role is server or agent
`)
}

func writeVersionUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage: atalaya version")
}

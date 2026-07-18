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
  backup   Reserved for data backup
  verify   Reserved for installation verification
  version  Print the Atalaya version
  help     Show command help
`)
}

func writeBootstrapUsage(output io.Writer) {
	fmt.Fprint(output, "Usage: atalaya bootstrap <command> [options]\n\nCommands:\n  local-agent  Bootstrap the local Oracle agent\n")
}

func writeBootstrapLocalAgentUsage(output io.Writer) {
	fmt.Fprint(output, `Usage: atalaya bootstrap local-agent --server-config <path> --agent-config <path> --agent-url <url> [options]

Required:
  --server-config <path>   Server YAML configuration
  --agent-config <path>    Agent YAML configuration to create or verify
  --agent-url <url>        Advertised https://<fqdn>.ts.net:9474 URL

Options:
  --display-name <name>    Host display name (default Oracle)
  --listen <addr>          Agent listener (default 127.0.0.1:9474)
  --credential-path <path> Credential file (default beside agent config)
`)
}

func writeAgentUsage(output io.Writer) {
	fmt.Fprint(output, "Usage: atalaya agent --config <path>\n\nOptions:\n  --config <path>  Agent YAML configuration (required)\n")
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

func writeReservedUsage(output io.Writer, command string) {
	fmt.Fprintf(output, "Usage: atalaya %s\n\nReserved for a later phase; execution is not implemented yet.\n", command)
}

func writeVersionUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage: atalaya version")
}

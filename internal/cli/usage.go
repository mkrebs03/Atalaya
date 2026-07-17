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
  agent    Reserved for the monitoring agent
  backup   Reserved for data backup
  verify   Reserved for installation verification
  version  Print the Atalaya version
  help     Show command help
`)
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

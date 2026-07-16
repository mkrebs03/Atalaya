package cli

import (
	"fmt"
	"io"
)

func writeRootUsage(output io.Writer) {
	fmt.Fprint(output, `Usage: atalaya <command> [options]

Commands:
  server   Start the Atalaya HTTP server
  agent    Reserved for the monitoring agent
  backup   Reserved for data backup
  verify   Reserved for installation verification
  version  Print the Atalaya version
  help     Show command help
`)
}

func writeServerUsage(output io.Writer) {
	fmt.Fprint(output, `Usage: atalaya server --public-url <origin> [options]

Options:
  --listen <address>     TCP loopback address (default 127.0.0.1:8080)
  --mode <mode>          production or development (default production)
  --public-url <origin>  Browser-facing origin; required
`)
}

func writeReservedUsage(output io.Writer, command string) {
	fmt.Fprintf(output, "Usage: atalaya %s\n\nReserved in Phase 0; execution is not implemented yet.\n", command)
}

func writeVersionUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage: atalaya version")
}

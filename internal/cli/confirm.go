package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

func confirmExport(input io.Reader, output io.Writer) (bool, error) {
	fmt.Fprint(output, "Exportar la clave maestra al archivo indicado? [y/N] ")
	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && len(line) == 0 {
		return false, err
	}
	answer := strings.TrimSpace(strings.ToLower(line))
	return answer == "y" || answer == "yes" || answer == "s" || answer == "si", nil
}

package main

import (
	"fmt"
	"os"

	"github.com/mkrebs03/atalaya/internal/cli"
	"github.com/mkrebs03/atalaya/web"
)

func main() {
	application, err := cli.New(web.Assets())
	if err != nil {
		fmt.Fprintf(os.Stderr, "atalaya: initialize: %v\n", err)
		os.Exit(1)
	}

	os.Exit(application.Run(os.Args[1:], os.Stdout, os.Stderr))
}

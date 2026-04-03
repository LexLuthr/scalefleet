package main

import (
	"fmt"
	"os"

	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:    "controller",
		Usage:   "Github action controller used for cloud based CI",
		Version: "0.0.1",
		Commands: []*cli.Command{
			runGCPCommand,
		},
	}
	app.Setup()
	if err := app.Run(os.Args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

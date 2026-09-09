package main

import (
	"os"

	"github.com/Achno2k/agents-cli/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}

package main

import (
	"fmt"
	"os"

	"github.com/uppu/jito/internal/cli"
)

var (
	version = "0.1.0"
	commit  = "dev"
	date    = "unknown"
)

func main() {
	root := cli.NewRootCmd(version, commit, date)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
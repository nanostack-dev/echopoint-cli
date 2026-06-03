package main

import (
	"errors"
	"fmt"
	"os"

	"echopoint-cli/internal/commands"
)

// Build metadata, injected by GoReleaser via -ldflags
// (-X main.version=... -X main.commit=... -X main.date=...).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// exitCoder is implemented by errors that carry a specific exit code.
type exitCoder interface {
	ExitCode() int
}

func main() {
	commands.SetBuildInfo(version, commit, date)
	root := commands.NewRootCmd()
	if err := root.Execute(); err != nil {
		var ec exitCoder
		if errors.As(err, &ec) {
			code := ec.ExitCode()
			if code != 0 {
				fmt.Fprintln(os.Stderr, err)
			}
			os.Exit(code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

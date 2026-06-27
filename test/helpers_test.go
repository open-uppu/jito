package test

import (
	"os/exec"
)

// realExec wraps os/exec.Command for use in test helpers.
func realExec(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
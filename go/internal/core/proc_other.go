//go:build !windows

package core

import "os/exec"

func configureCmd(cmd *exec.Cmd) {}

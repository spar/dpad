package compose

import (
	"os/exec"
	"time"
)

func configureCommand(cmd *exec.Cmd) { cmd.WaitDelay = 2 * time.Second }

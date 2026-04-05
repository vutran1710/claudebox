package serve

import (
	"time"

	"github.com/vutran1710/claudebox/internal/shell"
)

func execShell(cmd string) (string, error) {
	res, err := shell.RunShellTimeout(2*time.Minute, cmd)
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

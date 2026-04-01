package shell

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func Run(ctx context.Context, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	}
	return Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, err
}

func RunTimeout(timeout time.Duration, name string, args ...string) (Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return Run(ctx, name, args...)
}

func RunShell(ctx context.Context, script string) (Result, error) {
	return Run(ctx, "bash", "-c", script)
}

func RunShellTimeout(timeout time.Duration, script string) (Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return RunShell(ctx, script)
}

func Which(binary string) bool {
	_, err := exec.LookPath(binary)
	return err == nil
}

func FileExists(path string) bool {
	_, err := RunTimeout(5*time.Second, "test", "-f", path)
	return err == nil
}

func ProcessRunning(pattern string) bool {
	res, _ := RunTimeout(5*time.Second, "pgrep", "-f", pattern)
	return res.ExitCode == 0
}

func SystemdActive(service string) bool {
	res, _ := RunTimeout(5*time.Second, "systemctl", "is-active", service)
	return res.Stdout == "active\n" || res.Stdout == "active"
}

func GetPublicIP() string {
	res, err := RunTimeout(10*time.Second, "curl", "-sf", "https://ifconfig.me")
	if err != nil {
		r2, _ := RunShellTimeout(5*time.Second, "hostname -I | awk '{print $1}'")
		return fmt.Sprintf("%s", r2.Stdout)
	}
	return res.Stdout
}

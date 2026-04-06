package shell

import (
	"context"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	res, err := Run(context.Background(), "echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Stdout != "hello\n" {
		t.Errorf("expected 'hello\\n', got %q", res.Stdout)
	}
	if res.ExitCode != 0 {
		t.Errorf("expected exit 0, got %d", res.ExitCode)
	}
}

func TestRunTimeout(t *testing.T) {
	res, err := RunTimeout(5*time.Second, "echo", "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Stdout != "test\n" {
		t.Errorf("expected 'test\\n', got %q", res.Stdout)
	}
}

func TestRunShellTimeout(t *testing.T) {
	res, err := RunShellTimeout(5*time.Second, `echo "from shell"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Stdout != "from shell\n" {
		t.Errorf("expected 'from shell\\n', got %q", res.Stdout)
	}
}

func TestRunShellExitCode(t *testing.T) {
	res, _ := RunShellTimeout(5*time.Second, `exit 42`)
	if res.ExitCode != 42 {
		t.Errorf("expected exit 42, got %d", res.ExitCode)
	}
}

func TestWhich(t *testing.T) {
	if !Which("echo") {
		t.Error("expected echo to be found")
	}
	if Which("nonexistent-binary-xyz") {
		t.Error("expected nonexistent binary to not be found")
	}
}

func TestFileExists(t *testing.T) {
	// /etc/hosts always exists
	if !FileExists("/etc/hosts") {
		t.Error("expected /etc/hosts to exist")
	}
	if FileExists("/nonexistent/path/file") {
		t.Error("expected nonexistent file to not exist")
	}
}

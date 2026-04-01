package main

import (
	"fmt"
	"os"

	"github.com/vutran1710/claudebox/internal/activate"
	"github.com/vutran1710/claudebox/internal/setup"
	"github.com/vutran1710/claudebox/internal/status"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "setup":
		if err := setup.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "setup failed: %v\n", err)
			os.Exit(1)
		}
	case "activate":
		if err := activate.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "activate failed: %v\n", err)
			os.Exit(1)
		}
	case "status":
		status.Run()
	case "version", "--version", "-v":
		fmt.Printf("cbx %s\n", version)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("cbx — ClaudeBox CLI")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  cbx setup      Install tools, authenticate Claude, start VNC")
	fmt.Println("  cbx activate   Start am-server tunnel and message pollers")
	fmt.Println("  cbx status     Show status of all services")
	fmt.Println("  cbx version    Show version")
}

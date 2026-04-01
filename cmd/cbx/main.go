package main

import (
	"fmt"
	"os"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "setup":
		runSetup()
	case "activate":
		runActivate()
	case "status":
		runStatus()
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

func runSetup()    { fmt.Println("setup: not implemented") }
func runActivate() { fmt.Println("activate: not implemented") }
func runStatus()   { fmt.Println("status: not implemented") }

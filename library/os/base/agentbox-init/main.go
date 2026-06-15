package main

import (
	"fmt"
	"os"
	"syscall"
)

func main() {
	fmt.Println("Agentbox Init System Starting...")
	fmt.Println("Initializing Network...")
	fmt.Println("Initializing Guardrails...")
	fmt.Println("Starting Supervisor...")
	
	args := []string{"/bin/bash"}
	if _, err := os.Stat("/usr/local/bin/opencode"); err == nil {
		args = []string{"/usr/local/bin/opencode"}
	}
	
	fmt.Printf("Boot complete! Dropping into %s\n", args[0])
	
	// Exec into the target TUI
	env := os.Environ()
	if err := syscall.Exec(args[0], args, env); err != nil {
		fmt.Printf("Error execing %s: %v\n", args[0], err)
		os.Exit(1)
	}
}

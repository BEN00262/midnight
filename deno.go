package main

import (
	"os"
	"os/exec"
)

func install_deno_runtime() {
	// Check if Deno is installed
	if _, err := exec.LookPath("deno"); err == nil {
		return
	}

	// Define the PowerShell command to install Deno
	cmd := exec.Command("powershell", "-Command", "irm https://deno.land/install.ps1 | iex")

	// Run the command and capture the output
	if _, err := cmd.CombinedOutput(); err != nil {
		os.Exit(1)
	}
}

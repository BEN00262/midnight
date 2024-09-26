package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/takama/daemon"
)

const (

	// name of the service
	name        = SERVICE_NAME
	description = SERVICE_dESCRIPTION
)

// dependencies that are NOT required by the service, but might be used
var dependencies = []string{}

// Service has embedded daemon
type Service struct {
	daemon.Daemon
}

// Manage by daemon commands or run the daemon
func (service *Service) Manage() (string, error) {

	usage := "Usage: myservice install | remove | start | stop | status"

	// if received any kind of command, do it
	if len(os.Args) > 1 {
		command := os.Args[1]
		switch command {
		case "install":
			return service.Install()

		case "remove":
			return service.Remove()

		case "start":
			return service.Start()

		case "stop":
			return service.Stop()

		case "status":
			return service.Status()

		default:
			return usage, nil
		}
	}

	// Do something, call your goroutines, etc

	// Set up channel on which to send signal notifications.
	// We must use a buffered channel or risk missing the signal
	// if we're not ready to receive when the signal is sent.
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	RunProxy()

	// never happen, but need to complete code
	return usage, nil
}

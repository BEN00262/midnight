package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	SERVICE_NAME = "VoryPayPluginService"
)

type VoryPayPluginService struct {
	quit chan struct{}
}

func (m *VoryPayPluginService) Execute(args []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}

	// Create a channel to handle service shutdown
	m.quit = make(chan struct{})

	// Start the HTTP server in a separate goroutine
	go m.RunProxy()

	status <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	el, err := eventlog.Open(SERVICE_NAME)
	if err != nil {
		return false, 1
	}
	defer el.Close()

	el.Info(1, "VoryPayPluginService is running!")

loop:
	for {
		select {
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				el.Info(1, "VoryPayPluginService is stopping!")
				close(m.quit) // Signal the HTTP server to stop
				break loop
			default:
				el.Error(1, fmt.Sprintf("unexpected control request #%d", c))
			}
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}

	status <- svc.Status{State: svc.StopPending}
	return false, 0
}

func ExecuteService() {
	isInteractive, err := svc.IsWindowsService()

	if err != nil {
		log.Fatalf("failed to determine if we are running in an interactive session: %v", err)
	}

	if !isInteractive {
		runService(SERVICE_NAME, false)
		return
	}

	// If running interactively, simply install the service
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			err = installService(SERVICE_NAME, "My Go Windows Service with HTTP Server")
		case "remove":
			err = removeService(SERVICE_NAME)
		}
		if err != nil {
			log.Fatalf("Failed to handle service: %v", err)
		}
	} else {
		log.Println("Usage: go run main.go <install|remove>")
	}
}

func runService(name string, isDebug bool) {
	err := eventlog.InstallAsEventCreate(name, eventlog.Error|eventlog.Warning|eventlog.Info)

	if err != nil {
		log.Fatalf("Event log install failed: %v", err)
	}

	err = svc.Run(name, &VoryPayPluginService{})

	if err != nil {
		log.Fatalf("Service failed: %v", err)
	}
}

func installService(name, displayName string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("could not connect to service manager: %v", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %s already exists", name)
	}

	config := mgr.Config{
		DisplayName: displayName,
		StartType:   mgr.StartAutomatic,
	}
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not get executable path: %v", err)
	}

	s, err = m.CreateService(name, exePath, config)
	if err != nil {
		return fmt.Errorf("could not create service: %v", err)
	}
	defer s.Close()

	err = eventlog.InstallAsEventCreate(name, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil {
		s.Delete()
		return fmt.Errorf("could not install event log source: %v", err)
	}

	log.Printf("Service %s installed successfully", name)
	return nil
}

func removeService(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("could not connect to service manager: %v", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("could not access service: %v", err)
	}
	defer s.Close()

	err = s.Delete()
	if err != nil {
		return fmt.Errorf("could not delete service: %v", err)
	}

	err = eventlog.Remove(name)
	if err != nil {
		return fmt.Errorf("could not remove event log source: %v", err)
	}

	log.Printf("Service %s removed successfully", name)
	return nil
}

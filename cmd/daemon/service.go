//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	serviceName        = "WinMuxDaemon"
	serviceDisplayName = "WinMux Daemon"
	serviceDescription = "winmux daemon: persistent PowerShell sessions over ConPTY and a named pipe"
)

// installService instala o daemon como serviço Windows com início automático.
func installService() error {
	// Caminho absoluto do executável atual
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return fmt.Errorf("resolve absolute path: %w", err)
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Service Control Manager: %w", err)
	}
	defer m.Disconnect()

	// Verifica se já existe
	s, err := m.OpenService(serviceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %q already exists. Run --remove-service first", serviceName)
	}

	// Cria o serviço
	s, err = m.CreateService(
		serviceName,
		exePath,
		mgr.Config{
			StartType:        mgr.StartAutomatic,
			DisplayName:      serviceDisplayName,
			Description:      serviceDescription,
			ServiceStartName: "LocalSystem",
		},
	)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()

	fmt.Printf("✓ Service %q installed.\n", serviceName)
	fmt.Println("  Start: sc start WinMuxDaemon")
	fmt.Println("  Stop:  sc stop WinMuxDaemon")
	return nil
}

// removeService para e remove o serviço Windows.
func removeService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Service Control Manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %q not found: %w", serviceName, err)
	}
	defer s.Close()

	// Tenta parar antes de remover
	status, err := s.Query()
	if err == nil && status.State != svc.Stopped {
		s.Control(svc.Stop)
		// Aguarda até 5s para parar
		for i := 0; i < 10; i++ {
			time.Sleep(500 * time.Millisecond)
			status, err = s.Query()
			if err != nil || status.State == svc.Stopped {
				break
			}
		}
	}

	if err := s.Delete(); err != nil {
		return fmt.Errorf("remove service: %w", err)
	}

	fmt.Printf("✓ Service %q removed.\n", serviceName)
	return nil
}

// winmuxService implementa svc.Handler para rodar como serviço Windows.
type winmuxService struct {
	run func()
}

func (ws *winmuxService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown

	changes <- svc.Status{State: svc.StartPending}

	// Inicia o daemon em goroutine
	go ws.run()

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	for c := range r {
		switch c.Cmd {
		case svc.Interrogate:
			changes <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			changes <- svc.Status{State: svc.StopPending}
			return false, 0
		}
	}
	return false, 0
}

// runAsService detecta se está rodando como serviço e, se sim, executa nesse modo.
// Retorna true se estava rodando como serviço (main não deve continuar).
func runAsService(daemonRun func()) (bool, error) {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false, fmt.Errorf("detect service mode: %w", err)
	}
	if !isService {
		return false, nil
	}

	ws := &winmuxService{run: daemonRun}
	if err := svc.Run(serviceName, ws); err != nil {
		return true, fmt.Errorf("run as service: %w", err)
	}
	return true, nil
}

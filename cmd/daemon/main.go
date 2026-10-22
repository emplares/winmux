//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/emplares/winmux/internal/daemon"
	"github.com/emplares/winmux/internal/ipc"
)

func main() {
	// Detectar flags de gerenciamento de serviço
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--install-service":
			if err := installService(); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to install service: %v\n", err)
				os.Exit(1)
			}
			os.Exit(0)
		case "--remove-service":
			if err := removeService(); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to remove service: %v\n", err)
				os.Exit(1)
			}
			os.Exit(0)
		case "--help", "-h":
			printDaemonUsage()
			os.Exit(0)
		}
	}

	// Detectar se está rodando como serviço Windows
	wasService, err := runAsService(runDaemon)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Service mode error: %v\n", err)
		os.Exit(1)
	}
	if wasService {
		// Retornou do serviço normalmente — encerra
		return
	}

	// Modo normal (linha de comando)
	runDaemon()
}

// runDaemon inicializa e executa o loop principal do daemon.
func runDaemon() {
	fmt.Println("winmux daemon v" + ipc.Version)
	fmt.Printf("Named pipe: %s\n", ipc.PipeName)

	d := daemon.New()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nwinmux daemon shutting down...")
		os.Exit(0)
	}()

	listener, err := ipc.Listen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer listener.Close()

	fmt.Println("Waiting for connections...")
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to accept connection: %v\n", err)
			continue
		}
		go handleConn(d, conn)
	}
}

func handleConn(d *daemon.Daemon, conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var req ipc.Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}

		// Attach tem protocolo especial (streaming raw)
		if req.Action == "attach" {
			d.HandleAttach(req.ID, conn)
			return
		}

		resp, err := d.Handle(req)
		if err != nil {
			resp = map[string]string{"error": err.Error()}
		}
		data, _ := json.Marshal(resp)
		conn.Write(append(data, '\n'))
	}
}

func printDaemonUsage() {
	fmt.Println(`winmux-daemon: keeps PowerShell sessions alive in ConPTY behind a named pipe

Usage:
  winmux-daemon.exe                    Run in the foreground
  winmux-daemon.exe --install-service  Install as a Windows service (auto-start, LocalSystem)
  winmux-daemon.exe --remove-service   Remove the Windows service
  winmux-daemon.exe --help             Show this help

Manage the service with:
  sc start WinMuxDaemon
  sc stop  WinMuxDaemon
  sc query WinMuxDaemon`)
}

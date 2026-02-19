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

	"github.com/Microsoft/go-winio"
	"github.com/emplares/winmux/internal/daemon"
	"github.com/emplares/winmux/internal/ipc"
)

func main() {
	// Detectar flags de gerenciamento de serviço
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--install-service":
			if err := installService(); err != nil {
				fmt.Fprintf(os.Stderr, "Erro ao instalar serviço: %v\n", err)
				os.Exit(1)
			}
			os.Exit(0)
		case "--remove-service":
			if err := removeService(); err != nil {
				fmt.Fprintf(os.Stderr, "Erro ao remover serviço: %v\n", err)
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
		fmt.Fprintf(os.Stderr, "Erro no modo serviço: %v\n", err)
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
	fmt.Println("winmux daemon v0.2.0")
	fmt.Printf("Named pipe: %s\n", ipc.PipeName)

	d := daemon.New()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nwinmux daemon encerrando...")
		os.Exit(0)
	}()

	cfg := &winio.PipeConfig{
		InputBufferSize:  65536,
		OutputBufferSize: 65536,
	}
	listener, err := winio.ListenPipe(ipc.PipeName, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao criar named pipe: %v\n", err)
		os.Exit(1)
	}
	defer listener.Close()

	fmt.Println("Aguardando conexões...")
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao aceitar conexão: %v\n", err)
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
	fmt.Println(`winmux-daemon — daemon de sessões PowerShell via ConPTY + named pipes

Uso:
  winmux-daemon.exe                    Executa em modo normal (terminal)
  winmux-daemon.exe --install-service  Instala como serviço Windows (auto-start)
  winmux-daemon.exe --remove-service   Remove o serviço Windows
  winmux-daemon.exe --help             Exibe esta ajuda

Quando instalado como serviço, o daemon inicia automaticamente com o Windows.
Gerenciar com:
  sc start WinMuxDaemon
  sc stop  WinMuxDaemon
  sc query WinMuxDaemon`)
}

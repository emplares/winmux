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
	fmt.Println("winmux daemon v0.1.0-m2")
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

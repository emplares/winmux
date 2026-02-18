// winmux — M0 Spike
// Prova de conceito: criar sessão PowerShell via ConPTY, fazer I/O bidirecional.
// Compile para Windows: GOOS=windows GOARCH=amd64 go build -o winmux-spike.exe ./spike
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/UserExistsError/conpty"
)

func main() {
	fmt.Println("[winmux spike] Iniciando sessão PowerShell via ConPTY...")

	// Inicia PowerShell via ConPTY
	cpty, err := conpty.Start(`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[erro] Falha ao iniciar ConPTY: %v\n", err)
		os.Exit(1)
	}
	defer cpty.Close()

	fmt.Println("[winmux spike] Sessão iniciada. Ctrl+C para encerrar.")

	// Captura Ctrl+C para encerrar limpo
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// I/O bidirecional: stdout da sessão → terminal; terminal → stdin da sessão
	go func() {
		io.Copy(os.Stdout, cpty)
	}()
	go func() {
		io.Copy(cpty, os.Stdin)
	}()

	// Aguarda sinal de encerramento ou processo terminar
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-sigCh
		fmt.Println("\n[winmux spike] Encerrando...")
		cancel()
	}()

	// Aguarda processo terminar ou ctx cancelado
	doneCh := make(chan uint32, 1)
	go func() {
		code, err := cpty.Wait(ctx)
		if err != nil && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "[erro] Wait: %v\n", err)
		}
		doneCh <- code
	}()

	select {
	case code := <-doneCh:
		fmt.Printf("[winmux spike] Sessão encerrada com código: %d\n", code)
	case <-ctx.Done():
		fmt.Println("[winmux spike] Sessão interrompida.")
		time.Sleep(100 * time.Millisecond)
	}
}

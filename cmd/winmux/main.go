//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/emplares/winmux/internal/ipc"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "new":
		cmdNew()
	case "list":
		cmdList()
	case "kill":
		requireArg("kill", 3)
		cmdKill(os.Args[2])
	case "send":
		requireArg("send", 4)
		cmdSend(os.Args[2], strings.Join(os.Args[3:], " "))
	case "submit":
		requireArg("submit", 4)
		cmdSubmit(os.Args[2], strings.Join(os.Args[3:], " "))
	case "attach":
		requireArg("attach", 3)
		cmdAttach(os.Args[2])
	case "read":
		requireArg("read", 3)
		n := 50
		if len(os.Args) >= 4 {
			if parsed, err := strconv.Atoi(os.Args[3]); err == nil {
				n = parsed
			}
		}
		cmdRead(os.Args[2], n)
	case "status":
		requireArg("status", 3)
		cmdStatus(os.Args[2])
	case "reset":
		requireArg("reset", 3)
		cmdReset(os.Args[2])
	case "key":
		requireArg("key", 4)
		cmdKey(os.Args[2], os.Args[3])
	case "watch":
		requireArg("watch", 3)
		cmdWatch(os.Args[2:])
	case "version":
		fmt.Println("winmux v0.2.0")
	default:
		fmt.Fprintf(os.Stderr, "Comando desconhecido: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func cmdNew() {
	shell := "powershell"
	for i, arg := range os.Args {
		if (arg == "--shell" || arg == "-s") && i+1 < len(os.Args) {
			shell = os.Args[i+1]
		}
	}
	resp, err := send(ipc.Request{Action: "new", Shell: shell})
	if err != nil {
		fatal(err)
	}
	var r ipc.ResponseNew
	mustUnmarshal(resp, &r)
	if r.Error != "" {
		fatal(fmt.Errorf(r.Error))
	}
	fmt.Printf("✓ Sessão criada: %s (PID %d)\n", r.SessionID, r.PID)
	fmt.Printf("  winmux attach %s\n", r.SessionID)
}

func cmdList() {
	resp, err := send(ipc.Request{Action: "list"})
	if err != nil {
		fatal(err)
	}
	var r ipc.ResponseList
	mustUnmarshal(resp, &r)
	if r.Error != "" {
		fatal(fmt.Errorf(r.Error))
	}
	if len(r.Sessions) == 0 {
		fmt.Println("Nenhuma sessão ativa. Use: winmux new")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tPID\tSHELL\tSTATUS\tUPTIME\tDIR")
	fmt.Fprintln(w, "──\t───\t─────\t──────\t──────\t───")
	for _, s := range r.Sessions {
		uptime := time.Since(s.CreatedAt).Round(time.Second)
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\t%s\n",
			s.ID, s.PID, s.Shell, s.Status, uptime, s.Dir)
	}
	w.Flush()
}

func cmdKill(id string) {
	resp, err := send(ipc.Request{Action: "kill", ID: id})
	if err != nil {
		fatal(err)
	}
	var r ipc.ResponseKill
	mustUnmarshal(resp, &r)
	if r.Error != "" {
		fatal(fmt.Errorf(r.Error))
	}
	fmt.Printf("✓ Sessão %s encerrada.\n", id)
}

func cmdSend(id, input string) {
	resp, err := send(ipc.Request{Action: "send", ID: id, Input: input})
	if err != nil {
		fatal(err)
	}
	var r ipc.ResponseSend
	mustUnmarshal(resp, &r)
	if r.Error != "" {
		fatal(fmt.Errorf(r.Error))
	}
	fmt.Printf("✓ Comando enviado para sessão %s\n", id)
}

// cmdSubmit é como send mas com semântica explícita para TUIs interativas.
// Útil para Gemini CLI, Codex CLI, etc.
func cmdSubmit(id, input string) {
	resp, err := send(ipc.Request{Action: "submit", ID: id, Input: input})
	if err != nil {
		fatal(err)
	}
	var r ipc.ResponseSend
	mustUnmarshal(resp, &r)
	if r.Error != "" {
		fatal(fmt.Errorf(r.Error))
	}
	fmt.Printf("✓ Input submetido para sessão %s\n", id)
}

func cmdRead(id string, lines int) {
	resp, err := send(ipc.Request{Action: "read", ID: id, Lines: lines})
	if err != nil {
		fatal(err)
	}
	var r ipc.ResponseRead
	mustUnmarshal(resp, &r)
	if r.Error != "" {
		fatal(fmt.Errorf(r.Error))
	}
	for _, line := range r.Lines {
		fmt.Println(line)
	}
}

func cmdStatus(id string) {
	resp, err := send(ipc.Request{Action: "status", ID: id})
	if err != nil {
		fatal(err)
	}
	var r ipc.ResponseStatus
	mustUnmarshal(resp, &r)
	if r.Error != "" {
		fatal(fmt.Errorf(r.Error))
	}
	if r.Detail != "" {
		fmt.Printf("%s (%s)\n", r.State, r.Detail)
	} else {
		fmt.Println(r.State)
	}
}

func cmdReset(id string) {
	resp, err := send(ipc.Request{Action: "reset", ID: id})
	if err != nil {
		fatal(err)
	}
	var r ipc.ResponseSend
	mustUnmarshal(resp, &r)
	if r.Error != "" {
		fatal(fmt.Errorf(r.Error))
	}
	fmt.Printf("✓ Sessão %s resetada (Ctrl+C enviado)\n", id)
}

func cmdKey(id, key string) {
	resp, err := send(ipc.Request{Action: "key", ID: id, Key: key})
	if err != nil {
		fatal(err)
	}
	var r ipc.ResponseKey
	mustUnmarshal(resp, &r)
	if r.Error != "" {
		fatal(fmt.Errorf(r.Error))
	}
	fmt.Printf("✓ Tecla %q enviada para sessão %s\n", key, id)
}

// cmdWatch inicia um watcher que executa callbacks quando o estado muda.
// Uso: winmux watch <id> [--on-idle <cmd>] [--on-waiting <cmd>] [--timeout <segundos>]
func cmdWatch(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Uso: winmux watch <id> [--on-idle <cmd>] [--on-waiting <cmd>] [--timeout <seg>]")
		os.Exit(1)
	}
	id := args[0]
	req := ipc.Request{Action: "watch", ID: id}

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--on-idle":
			if i+1 < len(args) {
				req.OnIdle = args[i+1]
				i++
			}
		case "--on-waiting":
			if i+1 < len(args) {
				req.OnWait = args[i+1]
				i++
			}
		case "--timeout":
			if i+1 < len(args) {
				if t, err := strconv.Atoi(args[i+1]); err == nil {
					req.Timeout = t
				}
				i++
			}
		}
	}

	resp, err := send(req)
	if err != nil {
		fatal(err)
	}
	var r ipc.ResponseWatch
	mustUnmarshal(resp, &r)
	if r.Error != "" {
		fatal(fmt.Errorf(r.Error))
	}

	fmt.Printf("👀 Watching sessão %s", id)
	if req.OnIdle != "" {
		fmt.Printf(" | on-idle: %q", req.OnIdle)
	}
	if req.OnWait != "" {
		fmt.Printf(" | on-waiting: %q", req.OnWait)
	}
	fmt.Println()
	fmt.Println("   (O callback será executado pelo daemon em background)")
}

func cmdAttach(id string) {
	// Attach usa streaming raw — conecta diretamente ao pipe
	timeout := 3 * time.Second
	conn, err := winio.DialPipe(ipc.PipeName, &timeout)
	if err != nil {
		fatal(fmt.Errorf("conectar ao daemon: %w\nO daemon está rodando? Execute: winmux-daemon.exe", err))
	}
	defer conn.Close()

	// Envia request de attach
	req, _ := json.Marshal(ipc.Request{Action: "attach", ID: id})
	conn.Write(append(req, '\n'))

	// Lê confirmação JSON
	buf := make([]byte, 512)
	n, _ := conn.Read(buf)
	var r ipc.ResponseAttach
	json.Unmarshal(buf[:n], &r)
	if r.Error != "" {
		fatal(fmt.Errorf(r.Error))
	}

	fmt.Printf("[winmux] Conectado à sessão %s — Ctrl+C para desconectar\n", id)

	// Captura Ctrl+C para desconectar limpo
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	go func() {
		<-sigCh
		fmt.Println("\n[winmux] Desconectado. Sessão continua rodando.")
		conn.Close()
		os.Exit(0)
	}()

	// Streaming bidirecional
	go io.Copy(os.Stdout, conn)
	io.Copy(conn, os.Stdin)
}

// helpers

func send(req ipc.Request) ([]byte, error) {
	return ipc.SendRequest(req)
}

func mustUnmarshal(data []byte, v any) {
	if err := json.Unmarshal(data, v); err != nil {
		fatal(fmt.Errorf("resposta inválida do daemon: %v", err))
	}
}

func requireArg(cmd string, min int) {
	if len(os.Args) < min {
		fmt.Fprintf(os.Stderr, "Uso: winmux %s <id>\n", cmd)
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "Erro:", err)
	os.Exit(1)
}

func printUsage() {
	fmt.Println(`winmux — controle remoto de sessões PowerShell

Uso:
  winmux new [--shell powershell|cmd]          Cria nova sessão
  winmux list                                  Lista sessões ativas
  winmux attach <id>                           Conecta à sessão (I/O ao vivo)
  winmux send <id> <comando>                   Injeta comando (shell)
  winmux submit <id> <texto>                   Injeta texto em TUI (Gemini, Codex)
  winmux read <id> [linhas]                    Lê buffer de output (padrão: 50)
  winmux status <id>                           Estado: idle | busy | waiting
  winmux key <id> <tecla>                      Envia tecla especial
  winmux reset <id>                            Envia Ctrl+C (limpa >>)
  winmux watch <id> [--on-idle <cmd>]          Notifica quando estado muda
              [--on-waiting <cmd>]
              [--timeout <segundos>]
  winmux kill <id>                             Encerra sessão
  winmux version                               Versão

Exemplos:
  winmux submit abc123 "resuma esses arquivos"
  winmux key abc123 esc
  winmux key abc123 ctrl+c
  winmux key abc123 up
  winmux status abc123
  winmux watch abc123 --on-idle "curl -X POST https://meu-webhook.com/notify"
  winmux reset abc123

O daemon deve estar rodando: winmux-daemon.exe`)
}

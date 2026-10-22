//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

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
	case "logs":
		requireArg("logs", 3)
		cmdLogs(os.Args[2])
	case "watch":
		requireArg("watch", 3)
		cmdWatch(os.Args[2:])
	case "job":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Uso: winmux job <run|status|output|wait|list|kill> ...")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "run":
			requireJobArgs("job run", 5)
			cmdJobRun(os.Args[3], strings.Join(os.Args[4:], " "))
		case "status":
			requireJobArgs("job status", 4)
			cmdJobStatus(os.Args[3])
		case "output":
			requireJobArgs("job output", 4)
			cmdJobOutput(os.Args[3:])
		case "wait":
			requireJobArgs("job wait", 4)
			cmdJobWait(os.Args[3:])
		case "list":
			cmdJobList()
		case "kill":
			requireJobArgs("job kill", 4)
			cmdJobKill(os.Args[3])
		default:
			fmt.Fprintf(os.Stderr, "Subcomando job desconhecido: %s\n", os.Args[2])
			os.Exit(1)
		}
	case "version":
		fmt.Println("winmux v" + ipc.Version)
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
		fatal(errors.New(r.Error))
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
		fatal(errors.New(r.Error))
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
		fatal(errors.New(r.Error))
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
		fatal(errors.New(r.Error))
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
		fatal(errors.New(r.Error))
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
		fatal(errors.New(r.Error))
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
		fatal(errors.New(r.Error))
	}

	// Escolhe emoji/prefixo de acordo com o estado
	var prefix string
	switch r.State {
	case "idle":
		prefix = "✓"
	case "busy":
		prefix = "⏳"
	case "waiting":
		prefix = "⏸"
	case "error":
		prefix = "❌"
	default:
		prefix = "?"
	}

	if r.Detail != "" {
		fmt.Printf("%s %s (%s)\n", prefix, r.State, r.Detail)
	} else {
		fmt.Printf("%s %s\n", prefix, r.State)
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
		fatal(errors.New(r.Error))
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
		fatal(errors.New(r.Error))
	}
	fmt.Printf("✓ Tecla %q enviada para sessão %s\n", key, id)
}

// cmdLogs exibe o histórico completo do buffer de output de uma sessão (até 500 linhas).
func cmdLogs(id string) {
	resp, err := send(ipc.Request{Action: "logs", ID: id})
	if err != nil {
		fatal(err)
	}
	var r ipc.ResponseLogs
	mustUnmarshal(resp, &r)
	if r.Error != "" {
		fatal(errors.New(r.Error))
	}
	for _, line := range r.Lines {
		fmt.Println(line)
	}
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
		fatal(errors.New(r.Error))
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

// ── Job commands ──────────────────────────────────────────────────────────────

func requireJobArgs(cmd string, min int) {
	if len(os.Args) < min {
		fmt.Fprintf(os.Stderr, "Uso: winmux %s\n", cmd)
		os.Exit(1)
	}
}

func cmdJobRun(sessionID, command string) {
	resp, err := send(ipc.Request{Action: "job.run", ID: sessionID, Input: command})
	if err != nil {
		fatal(err)
	}
	var r ipc.ResponseJobRun
	mustUnmarshal(resp, &r)
	if r.Error != "" {
		fatal(errors.New(r.Error))
	}
	fmt.Printf("✓ Job iniciado: %s\n", r.JobID)
	fmt.Printf("  winmux job status %s\n", r.JobID)
	fmt.Printf("  winmux job output %s\n", r.JobID)
}

func cmdJobStatus(jobID string) {
	resp, err := send(ipc.Request{Action: "job.status", ID: jobID})
	if err != nil {
		fatal(err)
	}
	var r ipc.ResponseJobStatus
	mustUnmarshal(resp, &r)
	if r.Error != "" {
		fatal(errors.New(r.Error))
	}
	j := r.Job
	var prefix string
	switch j.State {
	case "running":
		prefix = "⏳"
	case "done":
		prefix = "✓"
	case "error":
		prefix = "❌"
	default:
		prefix = "?"
	}
	elapsed := time.Since(j.StartedAt).Round(time.Second)
	fmt.Printf("%s %s  sessão=%s  linhas=%d  tempo=%s\n", prefix, j.State, j.SessionID, j.Lines, elapsed)
	if j.Error != "" {
		fmt.Printf("  erro: %s\n", j.Error)
	}
}

func cmdJobOutput(args []string) {
	jobID := args[0]
	tail := 0
	for i := 1; i < len(args); i++ {
		if args[i] == "--tail" && i+1 < len(args) {
			if n, err := strconv.Atoi(args[i+1]); err == nil {
				tail = n
			}
			i++
		}
	}
	resp, err := send(ipc.Request{Action: "job.output", ID: jobID, Lines: tail})
	if err != nil {
		fatal(err)
	}
	var r ipc.ResponseJobOutput
	mustUnmarshal(resp, &r)
	if r.Error != "" {
		fatal(errors.New(r.Error))
	}
	for _, line := range r.Lines {
		fmt.Println(line)
	}
}

func cmdJobWait(args []string) {
	jobID := args[0]
	timeout := 0
	for i := 1; i < len(args); i++ {
		if args[i] == "--timeout" && i+1 < len(args) {
			// aceita "30m", "1h", ou segundos inteiros
			s := args[i+1]
			if d, err := time.ParseDuration(s); err == nil {
				timeout = int(d.Seconds())
			} else if n, err := strconv.Atoi(s); err == nil {
				timeout = n
			}
			i++
		}
	}
	fmt.Printf("⏳ Aguardando job %s", jobID)
	if timeout > 0 {
		fmt.Printf(" (timeout: %ds)", timeout)
	}
	fmt.Println("...")

	resp, err := send(ipc.Request{Action: "job.wait", ID: jobID, Timeout: timeout})
	if err != nil {
		fatal(err)
	}
	var r ipc.ResponseJobStatus
	mustUnmarshal(resp, &r)
	if r.Error != "" {
		fatal(errors.New(r.Error))
	}
	j := r.Job
	switch j.State {
	case "done":
		fmt.Printf("✓ Concluído em %s (%d linhas de output)\n", j.EndedAt.Sub(j.StartedAt).Round(time.Second), j.Lines)
	case "error":
		fmt.Printf("❌ Erro: %s\n", j.Error)
	case "running":
		fmt.Println("⏸ Timeout atingido — job ainda em execução.")
	}
}

func cmdJobList() {
	resp, err := send(ipc.Request{Action: "job.list"})
	if err != nil {
		fatal(err)
	}
	var r ipc.ResponseJobList
	mustUnmarshal(resp, &r)
	if r.Error != "" {
		fatal(errors.New(r.Error))
	}
	if len(r.Jobs) == 0 {
		fmt.Println("Nenhum job ativo.")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "JOB\tSESSÃO\tESTADO\tLINHAS\tTEMPO\tCOMANDO")
	fmt.Fprintln(w, "───\t──────\t──────\t──────\t─────\t───────")
	for _, j := range r.Jobs {
		elapsed := time.Since(j.StartedAt).Round(time.Second)
		cmd := j.Command
		if len(cmd) > 40 {
			cmd = cmd[:37] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n",
			j.ID, j.SessionID, j.State, j.Lines, elapsed, cmd)
	}
	w.Flush()
}

func cmdJobKill(jobID string) {
	resp, err := send(ipc.Request{Action: "job.kill", ID: jobID})
	if err != nil {
		fatal(err)
	}
	var r ipc.ResponseJobKill
	mustUnmarshal(resp, &r)
	if r.Error != "" {
		fatal(errors.New(r.Error))
	}
	fmt.Printf("✓ Job %s cancelado.\n", jobID)
}

func cmdAttach(id string) {
	// Attach usa streaming raw — conecta diretamente ao pipe
	conn, err := ipc.Dial()
	if err != nil {
		fatal(err)
	}
	defer conn.Close()

	req, _ := json.Marshal(ipc.Request{Action: "attach", ID: id})
	conn.Write(append(req, '\n'))

	// A confirmação é uma linha JSON; o que vier depois já é output da sessão.
	rd := bufio.NewReader(conn)
	line, err := rd.ReadBytes('\n')
	if err != nil {
		fatal(fmt.Errorf("sem resposta do daemon: %w", err))
	}
	var r ipc.ResponseAttach
	mustUnmarshal(line, &r)
	if r.Error != "" {
		fatal(errors.New(r.Error))
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
	go io.Copy(os.Stdout, rd)
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
  winmux logs <id>                             Histórico completo do buffer (até 500 linhas)
  winmux read <id> [linhas]                    Lê buffer de output (padrão: 50)
  winmux status <id>                           Estado: idle | busy | waiting
  winmux key <id> <tecla>                      Envia tecla especial
  winmux reset <id>                            Envia Ctrl+C (limpa >>)
  winmux watch <id> [--on-idle <cmd>]          Notifica quando estado muda
              [--on-waiting <cmd>]
              [--timeout <segundos>]
  winmux kill <id>                             Encerra sessão

Jobs assíncronos (modo agente):
  winmux job run <id> <comando>                Dispara comando async, retorna job ID
  winmux job status <job-id>                   Estado: running | done | error
  winmux job output <job-id> [--tail N]        Output acumulado do job
  winmux job wait <job-id> [--timeout 30m]     Aguarda conclusão com timeout
  winmux job list                              Lista todos os jobs ativos
  winmux job kill <job-id>                     Cancela job em andamento

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

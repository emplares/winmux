//go:build windows

package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

const PipeName = `\\.\pipe\winmux`

// Listen abre o named pipe do daemon. A DACL é explícita: só SYSTEM,
// Administrators e o usuário que roda o daemon conectam. Sem ela o Windows
// aplica a ACL padrão de pipes, que também dá leitura a Everyone e Anonymous.
// Clientes remotos (SMB) já são recusados pelo go-winio.
func Listen() (net.Listener, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("identificar usuário do daemon: %w", err)
	}
	sddl := "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;" + user.User.Sid.String() + ")"
	l, err := winio.ListenPipe(PipeName, &winio.PipeConfig{
		SecurityDescriptor: sddl,
		InputBufferSize:    65536,
		OutputBufferSize:   65536,
	})
	if err != nil {
		return nil, fmt.Errorf("criar named pipe %s: %w", PipeName, err)
	}
	return l, nil
}

// Dial conecta ao daemon.
func Dial() (net.Conn, error) {
	timeout := 3 * time.Second
	conn, err := winio.DialPipe(PipeName, &timeout)
	if err != nil {
		return nil, fmt.Errorf("conectar ao daemon: %w\nO daemon está rodando? Execute: winmux-daemon.exe", err)
	}
	return conn, nil
}

// SendRequest envia um request ao daemon e retorna a resposta JSON raw.
func SendRequest(req Request) ([]byte, error) {
	conn, err := Dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	data, _ := json.Marshal(req)
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024) // logs/job output passam de 64KB
	if scanner.Scan() {
		return scanner.Bytes(), nil
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ler resposta do daemon: %w", err)
	}
	return nil, fmt.Errorf("sem resposta do daemon")
}

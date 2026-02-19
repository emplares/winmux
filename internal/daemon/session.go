//go:build windows

package daemon

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/UserExistsError/conpty"
)

var shellPaths = map[string]string{
	"powershell": `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
	"cmd":        `C:\Windows\System32\cmd.exe`,
}

// Session representa uma sessão ConPTY ativa.
type Session struct {
	ID        string
	PID       int
	Shell     string
	Dir       string
	CreatedAt time.Time
	Status    string // "active", "dead"

	cpty   *conpty.ConPty
	cancel context.CancelFunc

	sessionBuffer // buffer circular de output + DetectState
}

// NewSession cria e inicia uma sessão ConPTY.
func NewSession(id, shell string) (*Session, error) {
	path, ok := shellPaths[shell]
	if !ok {
		path = shell
	}
	if path == "" {
		path = shellPaths["powershell"]
	}

	cpty, err := conpty.Start(path)
	if err != nil {
		return nil, fmt.Errorf("iniciar ConPTY (%s): %w", path, err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	s := &Session{
		ID:            id,
		PID:           cpty.Pid(),
		Shell:         shell,
		Dir:           `C:\`,
		CreatedAt:     time.Now(),
		Status:        "active",
		cpty:          cpty,
		cancel:        cancel,
		sessionBuffer: newSessionBuffer(),
	}

	go s.captureOutput(ctx)

	go func() {
		cpty.Wait(ctx)
		s.Status = "dead"
		cancel()
	}()

	return s, nil
}

// captureOutput lê o output do ConPTY e armazena no buffer circular.
func (s *Session) captureOutput(ctx context.Context) {
	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := s.cpty.Read(buf)
		if n > 0 {
			s.appendOutput(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// ReadBuffer retorna as últimas n linhas do buffer de output.
func (s *Session) ReadBuffer(n int) []string {
	return s.readBuffer(n)
}

// Logs retorna o histórico completo do buffer (últimas 500 linhas).
func (s *Session) Logs() []string {
	return s.logs()
}

// Send injeta texto + Enter na sessão.
func (s *Session) Send(input string) error {
	_, err := s.cpty.Write([]byte(input + "\r\n"))
	return err
}

// Submit injeta texto + Enter — semântica explícita para TUIs.
func (s *Session) Submit(input string) error {
	return s.Send(input)
}

// Reset envia Ctrl+C seguido de Enter para limpar estado de continuação ">>" no PowerShell.
func (s *Session) Reset() error {
	if _, err := s.cpty.Write([]byte{0x03}); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)
	_, err := s.cpty.Write([]byte("\r\n"))
	return err
}

// keySequences mapeia nomes de teclas para sequências de bytes.
var keySequences = map[string][]byte{
	"esc":        {0x1b},
	"escape":     {0x1b},
	"ctrl+c":     {0x03},
	"ctrl+d":     {0x04},
	"ctrl+z":     {0x1a},
	"ctrl+l":     {0x0c},
	"ctrl+a":     {0x01},
	"ctrl+e":     {0x05},
	"tab":        {0x09},
	"enter":      {0x0d},
	"up":         {0x1b, 0x5b, 0x41},
	"down":       {0x1b, 0x5b, 0x42},
	"right":      {0x1b, 0x5b, 0x43},
	"left":       {0x1b, 0x5b, 0x44},
	"home":       {0x1b, 0x5b, 0x48},
	"end":        {0x1b, 0x5b, 0x46},
	"pgup":       {0x1b, 0x5b, 0x35, 0x7e},
	"pgdn":       {0x1b, 0x5b, 0x36, 0x7e},
	"delete":     {0x1b, 0x5b, 0x33, 0x7e},
	"backspace":  {0x7f},
}

// SendKey envia uma tecla especial para a sessão pelo nome.
func (s *Session) SendKey(key string) error {
	seq, ok := keySequences[strings.ToLower(key)]
	if !ok {
		return fmt.Errorf("tecla desconhecida: %q. Use: esc, ctrl+c, ctrl+d, tab, enter, up, down, left, right, home, end, pgup, pgdn, backspace", key)
	}
	_, err := s.cpty.Write(seq)
	return err
}

// Attach conecta um writer ao output e um reader ao input da sessão (streaming).
func (s *Session) Attach(w io.Writer, r io.Reader, done chan struct{}) {
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := s.cpty.Read(buf)
			if n > 0 {
				w.Write(buf[:n])
			}
			if err != nil {
				close(done)
				return
			}
		}
	}()
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				s.cpty.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
}

// Close encerra a sessão.
func (s *Session) Close() {
	s.cancel()
	s.cpty.Close()
	s.Status = "dead"
}

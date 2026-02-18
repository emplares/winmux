//go:build windows

package daemon

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/UserExistsError/conpty"
)

const bufferMaxLines = 10000

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

	// Buffer circular de output (últimas N linhas)
	mu              sync.RWMutex
	buffer          []string
	line            []byte
	LastStateChange time.Time
	lastState       string
}

var shellPaths = map[string]string{
	"powershell": `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
	"cmd":        `C:\Windows\System32\cmd.exe`,
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
		ID:              id,
		PID:             cpty.Pid(),
		Shell:           shell,
		Dir:             `C:\`,
		CreatedAt:       time.Now(),
		Status:          "active",
		cpty:            cpty,
		cancel:          cancel,
		buffer:          make([]string, 0, 256),
		LastStateChange: time.Now(),
		lastState:       "idle",
	}

	// Captura output e armazena no buffer
	go s.captureOutput(ctx)

	// Monitora encerramento do processo
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
			if err != io.EOF {
				return
			}
			return
		}
	}
}

// appendOutput adiciona bytes ao buffer de linhas.
func (s *Session) appendOutput(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range data {
		if b == '\n' {
			s.buffer = append(s.buffer, string(s.line))
			s.line = s.line[:0]
			if len(s.buffer) > bufferMaxLines {
				s.buffer = s.buffer[1:]
			}
		} else {
			s.line = append(s.line, b)
		}
	}
}

// ReadBuffer retorna as últimas n linhas do buffer de output.
func (s *Session) ReadBuffer(n int) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n <= 0 || n > len(s.buffer) {
		n = len(s.buffer)
	}
	lines := make([]string, n)
	copy(lines, s.buffer[len(s.buffer)-n:])
	return lines
}

// Send injeta texto + Enter na sessão.
func (s *Session) Send(input string) error {
	_, err := s.cpty.Write([]byte(input + "\r\n"))
	return err
}

// Submit injeta texto + Enter explícito — idêntico ao Send mas semântica clara para TUIs.
// Útil para Gemini CLI, Codex CLI e outros TUIs que precisam de Enter para submeter.
func (s *Session) Submit(input string) error {
	return s.Send(input)
}

// Reset envia Ctrl+C seguido de Enter para limpar estado de continuação ">>" no PowerShell.
func (s *Session) Reset() error {
	// Ctrl+C
	if _, err := s.cpty.Write([]byte{0x03}); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)
	// Enter para limpar prompt
	_, err := s.cpty.Write([]byte("\r\n"))
	return err
}

// stripAnsi remove sequências de escape ANSI de uma string.
func stripAnsi(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		_ = r
		if s[i] == '\x1b' {
			// Pula sequência ESC
			i++
			if i < len(s) && s[i] == '[' {
				// CSI sequence: ESC [ ... final
				i++
				for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
					i++
				}
				if i < len(s) {
					i++ // consome final
				}
			} else if i < len(s) && s[i] == ']' {
				// OSC sequence: ESC ] ... BEL or ST
				i++
				for i < len(s) && s[i] != '\x07' && !(s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '\\') {
					i++
				}
				if i < len(s) {
					i++
				}
			}
		} else {
			b.WriteByte(s[i])
			i += size
		}
	}
	return b.String()
}

// spinnerChars são os caracteres braille de spinner usados por CLIs modernos.
const spinnerChars = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"

// asciiSpinnerChars são os spinners ASCII usados pelo npm/outros.
const asciiSpinnerChars = `\|/-`

// waitingPhrases são frases que indicam que a sessão aguarda input do usuário.
var waitingPhrases = []string{
	"do you trust",
	"press enter to continue",
	"press enter",
	"(y/n)",
	"[y/n]",
	"trust folder",
	"trust parent",
	"don't trust",
	"update available",
	"? for shortcuts",
	"press esc",
}

// DetectState analisa o buffer recente e retorna o estado da sessão.
// Retorna: ("idle"|"busy"|"waiting", detail)
func (s *Session) DetectState() (state, detail string) {
	s.mu.RLock()
	lines := s.buffer
	s.mu.RUnlock()

	if len(lines) == 0 {
		return "idle", ""
	}

	// Analisa as últimas 20 linhas (ou menos)
	lookback := 20
	if len(lines) < lookback {
		lookback = len(lines)
	}
	recent := lines[len(lines)-lookback:]

	// Junta tudo numa string para busca
	combined := strings.ToLower(strings.Join(recent, "\n"))
	combinedStripped := stripAnsi(combined)

	// 1. Detecta spinner (busy)
	for _, ch := range spinnerChars {
		if strings.ContainsRune(combined, ch) {
			// Tenta extrair tempo restante (ex: "5m 44s")
			detail = extractTimeHint(combinedStripped)
			return "busy", detail
		}
	}

	// Spinner ASCII: só conta se aparecer múltiplas vezes seguidas (padrão de animação)
	asciiSpinCount := 0
	for _, ch := range asciiSpinnerChars {
		asciiSpinCount += strings.Count(combined, string(ch))
	}
	if asciiSpinCount > 10 {
		detail = extractTimeHint(combinedStripped)
		return "busy", detail
	}

	// 2. Detecta waiting (aguardando input)
	for _, phrase := range waitingPhrases {
		if strings.Contains(combinedStripped, phrase) {
			// Extrai opções visíveis se existirem
			detail = extractOptions(recent)
			return "waiting", detail
		}
	}

	// 3. Detecta modo de continuação ">>" do PowerShell
	lastLine := stripAnsi(strings.TrimSpace(lines[len(lines)-1]))
	if lastLine == ">>" || strings.HasSuffix(lastLine, ">> ") {
		return "waiting", "PowerShell continuation mode (>>). Use: winmux reset <id>"
	}

	// 4. Idle — prompt disponível
	return "idle", ""
}

// extractTimeHint tenta extrair uma estimativa de tempo de uma string (ex: "5m 44s").
func extractTimeHint(s string) string {
	// Procura padrões como "5m 44s", "2m", "30s"
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 1 && w[len(w)-1] == 's' && isDigits(w[:len(w)-1]) {
			if i > 0 && len(words[i-1]) > 1 && words[i-1][len(words[i-1])-1] == 'm' {
				return words[i-1] + " " + w + " remaining"
			}
			return w + " remaining"
		}
	}
	return ""
}

// extractOptions extrai opções numeradas visíveis do buffer (ex: "1. Trust folder").
func extractOptions(lines []string) string {
	var opts []string
	for _, l := range lines {
		stripped := stripAnsi(strings.TrimSpace(l))
		if len(stripped) > 2 && (stripped[0] >= '1' && stripped[0] <= '9') && stripped[1] == '.' {
			opts = append(opts, stripped)
		}
	}
	if len(opts) > 0 {
		return strings.Join(opts, " | ")
	}
	return ""
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

// Attach conecta um writer ao output e um reader ao input da sessão (streaming).
func (s *Session) Attach(w io.Writer, r io.Reader, done chan struct{}) {
	// Output da sessão → writer (cliente)
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
	// Input do reader (cliente) → stdin da sessão
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

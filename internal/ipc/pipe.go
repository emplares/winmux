//go:build windows

package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

const PipeName = `\\.\pipe\winmux`

// Server escuta em um named pipe e processa conexões.
type Server struct {
	handler func(req Request) (any, error)
}

// NewServer cria um novo servidor de named pipe.
func NewServer(handler func(req Request) (any, error)) *Server {
	return &Server{handler: handler}
}

// Listen inicia o servidor e bloqueia aceitando conexões.
func (s *Server) Listen() error {
	cfg := &winio.PipeConfig{
		InputBufferSize:  65536,
		OutputBufferSize: 65536,
	}
	listener, err := winio.ListenPipe(PipeName, cfg)
	if err != nil {
		return fmt.Errorf("criar named pipe %s: %w", PipeName, err)
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			return fmt.Errorf("aceitar conexão: %w", err)
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var req Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		resp, err := s.handler(req)
		if err != nil {
			resp = map[string]string{"error": err.Error()}
		}
		data, _ := json.Marshal(resp)
		conn.Write(append(data, '\n'))
	}
}

// SendRequest envia um request ao daemon e retorna a resposta JSON raw.
func SendRequest(req Request) ([]byte, error) {
	timeout := 3 * time.Second
	conn, err := winio.DialPipe(PipeName, &timeout)
	if err != nil {
		return nil, fmt.Errorf("conectar ao daemon: %w\nO daemon está rodando? Execute: winmux-daemon.exe", err)
	}
	defer conn.Close()

	data, _ := json.Marshal(req)
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(conn)
	if scanner.Scan() {
		return scanner.Bytes(), nil
	}
	return nil, fmt.Errorf("sem resposta do daemon")
}

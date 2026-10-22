//go:build !windows

// session_factory_linux.go — stub da factory de sessões para sistemas não-Windows.
// Em Linux, não há ConPTY real; apenas testes com mocks são suportados.
package daemon

import "fmt"

// defaultSessionFactory é a factory padrão no Linux: sempre retorna erro.
// Os testes devem usar NewWithFactory com uma mock factory.
func defaultSessionFactory(id, shell string) (ISession, error) {
	return nil, fmt.Errorf("ConPTY is not available on this OS; use NewWithFactory with a mock in tests")
}

//go:build !windows

package main

import "fmt"

// installService não é suportado fora do Windows.
func installService() error {
	return fmt.Errorf("instalação de serviço disponível apenas no Windows")
}

// removeService não é suportado fora do Windows.
func removeService() error {
	return fmt.Errorf("remoção de serviço disponível apenas no Windows")
}

// runAsService retorna sempre false em sistemas não-Windows.
func runAsService(_ func()) (bool, error) {
	return false, nil
}

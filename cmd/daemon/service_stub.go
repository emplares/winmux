//go:build !windows

package main

import "fmt"

// installService não é suportado fora do Windows.
func installService() error {
	return fmt.Errorf("service install is only available on Windows")
}

// removeService não é suportado fora do Windows.
func removeService() error {
	return fmt.Errorf("service removal is only available on Windows")
}

// runAsService retorna sempre false em sistemas não-Windows.
func runAsService(_ func()) (bool, error) {
	return false, nil
}

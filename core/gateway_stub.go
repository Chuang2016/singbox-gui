//go:build !linux

package core

import "fmt"

func SetupGateway(tproxyPort int) error {
	return fmt.Errorf("gateway mode is only supported on Linux")
}

func TeardownGateway() error { return nil }

func GatewayActive() bool { return false }

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Per web/playwright.config.ts, which reserves free ports once and exports them to the harness.
const (
	applicationPortVariable = "E2E_APP_PORT"
	targetPortVariable      = "E2E_TARGET_PORT"
)

// portFromEnvironment reads a TCP port assigned in the named environment variable.
func portFromEnvironment(name string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("%s must name a TCP port between 1 and 65535", name)
	}
	return port, nil
}

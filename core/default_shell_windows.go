//go:build windows

package core

import (
	"os"
	"strings"
)

func currentAccountShell() string {
	return strings.TrimSpace(os.Getenv("COMSPEC"))
}

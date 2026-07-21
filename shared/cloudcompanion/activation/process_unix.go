//go:build !windows

package activation

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

func configureDetachedProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func smokeEndpoint() (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	runtimeDir := filepath.Join(os.TempDir(), "muxvia-"+strconv.Itoa(os.Getuid()))
	return filepath.Join(runtimeDir, fmt.Sprintf("cloud-smoke-%s.sock", hex.EncodeToString(random))), nil
}

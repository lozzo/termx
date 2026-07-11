//go:build windows

package activation

import (
	"crypto/rand"
	"encoding/hex"
	"os/exec"
	"syscall"

	"github.com/lozzow/termx/shared/cloudcompanion/ipc"
)

func configureDetachedProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008}
}

func smokeEndpoint() (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return ipc.DefaultEndpoint() + "-smoke-" + hex.EncodeToString(random), nil
}

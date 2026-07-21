// Package systemadapter 实现 TUI 对宿主系统能力的 port。
package systemadapter

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/muxvia/muxvia/tui/port"
)

// ClipboardService 通过宿主系统命令实现剪贴板读写。
// 它只拥有当前进程最近一次成功提交的文本副本；系统剪贴板内容仍由宿主平台持有。
type ClipboardService struct {
	lastCopied string
}

const clipboardCommandTimeout = 1500 * time.Millisecond

// Read 从当前宿主平台的剪贴板读取文本；没有可用命令时明确失败，不回退到 TUI 状态。
func (service *ClipboardService) Read(ctx context.Context) (port.ClipboardReadResult, error) {
	readCtx, cancel := context.WithTimeout(ctx, clipboardCommandTimeout)
	defer cancel()
	for _, spec := range clipboardReadCommands() {
		cmd := exec.CommandContext(readCtx, spec.name, spec.args...)
		out, err := cmd.Output()
		if err == nil {
			return port.ClipboardReadResult{Text: string(out)}, nil
		}
	}
	return port.ClipboardReadResult{}, fmt.Errorf("no system clipboard command available")
}

// Write 把文本写入当前宿主平台剪贴板；没有可用命令时明确失败。
func (service *ClipboardService) Write(ctx context.Context, req port.ClipboardWriteRequest) error {
	service.lastCopied = req.Text
	if req.Text == "" {
		return nil
	}
	writeCtx, cancel := context.WithTimeout(ctx, clipboardCommandTimeout)
	defer cancel()
	for _, spec := range clipboardWriteCommands() {
		cmd := exec.CommandContext(writeCtx, spec.name, spec.args...)
		cmd.Stdin = bytes.NewBufferString(req.Text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no system clipboard command available")
}

// LastCopy 返回该 adapter 最近一次接收的写入文本，用于 TUI 内部粘贴投影。
func (service *ClipboardService) LastCopy() string {
	return service.lastCopied
}

type clipboardCommandSpec struct {
	name string
	args []string
}

func clipboardWriteCommands() []clipboardCommandSpec {
	return []clipboardCommandSpec{
		{name: "wl-copy"},
		{name: "xclip", args: []string{"-selection", "clipboard", "-in"}},
		{name: "xsel", args: []string{"--clipboard", "--input"}},
		{name: "pbcopy"},
	}
}

func clipboardReadCommands() []clipboardCommandSpec {
	return []clipboardCommandSpec{
		{name: "wl-paste"},
		{name: "xclip", args: []string{"-selection", "clipboard", "-out"}},
		{name: "xsel", args: []string{"--clipboard", "--output"}},
		{name: "pbpaste"},
	}
}

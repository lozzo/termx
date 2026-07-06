package termxcorev2

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lozzow/termx/internal/protocol"
)

const defaultPathListDirsLimit = 100

func listPathDirectories(params protocol.PathListDirsParams) (protocol.PathListDirsResult, error) {
	prefix := params.Prefix
	if strings.TrimSpace(prefix) == "" {
		return protocol.PathListDirsResult{}, nil
	}
	baseDisplay, baseResolved, fragment, ok := pathCompletionBase(prefix)
	if !ok {
		return protocol.PathListDirsResult{}, nil
	}
	out := protocol.PathListDirsResult{BasePath: baseResolved}
	entries, err := os.ReadDir(baseResolved)
	if err != nil {
		// 中文说明：目录补全的 domain owner 是当前 daemon 机器文件系统；
		// base 不存在或无权限是 prompt 空态，不是 transport/endpoint lifecycle 失败。
		out.Missing = true
		return out, nil
	}
	limit := params.Limit
	if limit <= 0 {
		limit = defaultPathListDirsLimit
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})
	fragmentLower := strings.ToLower(fragment)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(fragment, ".") {
			continue
		}
		if fragmentLower != "" && !strings.HasPrefix(strings.ToLower(name), fragmentLower) {
			continue
		}
		if len(out.Entries) >= limit {
			out.Truncated = true
			break
		}
		candidate := name + string(filepath.Separator)
		if baseDisplay != "" {
			candidate = baseDisplay + candidate
		}
		out.Entries = append(out.Entries, protocol.PathDirEntry{Name: name, Path: candidate})
	}
	return out, nil
}

func pathDefaults() protocol.PathDefaultsResult {
	return protocol.PathDefaultsResult{
		DefaultCommand: defaultPathCommand(),
		DefaultCWD:     defaultPathCWD(),
	}
}

func defaultPathCommand() []string {
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell != "" {
		return []string{shell}
	}
	// 中文说明：默认 shell 的 truth 在 daemon 进程所在机器；环境缺失时只退回
	// 该机器上最小 POSIX shell，不读取 TUI/client 进程环境。
	return []string{"/bin/sh"}
}

func defaultPathCWD() string {
	cwd, err := os.Getwd()
	if err == nil && strings.TrimSpace(cwd) != "" {
		return strings.TrimSpace(cwd)
	}
	home, err := os.UserHomeDir()
	if err == nil {
		return strings.TrimSpace(home)
	}
	return ""
}

func pathCompletionBase(prefix string) (string, string, string, bool) {
	home, _ := os.UserHomeDir()
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", "", false
	}
	switch {
	case prefix == "~":
		if strings.TrimSpace(home) == "" {
			return "", "", "", false
		}
		return "~/", home, "", true
	case strings.HasPrefix(prefix, "~/"):
		if strings.TrimSpace(home) == "" {
			return "", "", "", false
		}
		rest := strings.TrimPrefix(prefix, "~/")
		base, fragment := splitPathCompletionPrefix(rest)
		return "~/" + base, filepath.Join(home, filepath.FromSlash(base)), fragment, true
	case strings.HasPrefix(prefix, "/"):
		base, fragment := splitPathCompletionPrefix(strings.TrimPrefix(prefix, "/"))
		return "/" + base, filepath.Join(string(filepath.Separator), filepath.FromSlash(base)), fragment, true
	default:
		base, fragment := splitPathCompletionPrefix(prefix)
		return base, filepath.Join(cwd, filepath.FromSlash(base)), fragment, true
	}
}

func splitPathCompletionPrefix(prefix string) (string, string) {
	lastSlash := strings.LastIndex(prefix, "/")
	if lastSlash < 0 {
		return "", prefix
	}
	return prefix[:lastSlash+1], prefix[lastSlash+1:]
}

func pathListDirsProtocolError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("path list dirs: %w", err)
}

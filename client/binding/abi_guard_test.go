package binding

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var expectedCSymbols = []string{
	"muxvia_client_abi_version", "muxvia_engine_create", "muxvia_engine_open_session", "muxvia_engine_execute",
	"muxvia_engine_open_resource_stream", "muxvia_engine_send_resource_stream_frame", "muxvia_engine_close_resource_stream",
	"muxvia_engine_command", "muxvia_engine_next_event",
	"muxvia_platform_next_request", "muxvia_platform_complete", "muxvia_engine_cancel", "muxvia_engine_close_session", "muxvia_engine_release",
	"muxvia_engine_close", "muxvia_buffer_free",
}

func TestBindingABIBaselinesStayGeneric(t *testing.T) {
	header, err := os.ReadFile("cabi/muxvia_client.h")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(header), "MUXVIA_CLIENT_ABI_VERSION 3u") || ABIVersion != 3 {
		t.Fatalf("ABI version mismatch header=%q go=%d", header, ABIVersion)
	}
	re := regexp.MustCompile(`(?m)^muxvia_status_v1 (muxvia_[a-z0-9_]+)\(`)
	symbols := []string{"muxvia_client_abi_version"}
	for _, match := range re.FindAllStringSubmatch(string(header), -1) {
		symbols = append(symbols, match[1])
	}
	if !slices.Equal(symbols, expectedCSymbols) {
		t.Fatalf("C symbols = %v, want %v", symbols, expectedCSymbols)
	}
	for _, forbidden := range []string{"terminal", "history", "file", "storage", "json", "base64"} {
		if strings.Contains(strings.ToLower(string(header)), forbidden) {
			t.Fatalf("binding ABI contains business/encoding term %q", forbidden)
		}
	}
}

func TestBindingCoreDoesNotImportPlatformOrDomainOwners(t *testing.T) {
	forbidden := []string{
		"C", "unsafe", "syscall/js", "encoding/json", "encoding/base64",
		"github.com/muxvia/muxvia/core", "github.com/muxvia/muxvia/tui", "github.com/muxvia/muxvia/cmd/muxvia",
		"github.com/muxvia/muxvia/private", "github.com/muxvia/muxvia/internal/protocol",
		"github.com/muxvia/muxvia/remote", "github.com/pion/webrtc",
	}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != "." && (entry.Name() == "cabi" || entry.Name() == "enginehost") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, "_test.go") || !strings.HasSuffix(path, ".go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range parsed.Imports {
			importPath := strings.Trim(imported.Path.Value, `"`)
			for _, prefix := range forbidden {
				if importPath == prefix || strings.HasPrefix(importPath, prefix+"/") {
					t.Errorf("binding core %s imports forbidden owner %s", path, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

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
	"termx_client_abi_version", "termx_engine_create", "termx_engine_open_session", "termx_engine_execute",
	"termx_engine_open_resource_stream", "termx_engine_send_resource_stream_frame", "termx_engine_close_resource_stream",
	"termx_engine_command", "termx_engine_next_event",
	"termx_platform_next_request", "termx_platform_complete", "termx_engine_cancel", "termx_engine_close_session", "termx_engine_release",
	"termx_engine_close", "termx_buffer_free",
}

var expectedWASMExports = []string{
	"termxClientAbiVersion", "termxEngineCreate", "termxEngineOpenSession", "termxEngineExecute",
	"termxEngineOpenResourceStream", "termxEngineSendResourceStreamFrame", "termxEngineCloseResourceStream",
	"termxEngineCommand", "termxEngineNextEvent",
	"termxPlatformNextRequest", "termxPlatformComplete", "termxPlatformEvent", "termxEngineCancel", "termxEngineCloseSession", "termxEngineRelease",
	"termxEngineClose", "termxBufferFree",
}

func TestBindingABIBaselinesStayGeneric(t *testing.T) {
	header, err := os.ReadFile("cabi/termx_client.h")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(header), "TERMX_CLIENT_ABI_VERSION 3u") || ABIVersion != 3 {
		t.Fatalf("ABI version mismatch header=%q go=%d", header, ABIVersion)
	}
	re := regexp.MustCompile(`(?m)^termx_status_v1 (termx_[a-z0-9_]+)\(`)
	symbols := []string{"termx_client_abi_version"}
	for _, match := range re.FindAllStringSubmatch(string(header), -1) {
		symbols = append(symbols, match[1])
	}
	if !slices.Equal(symbols, expectedCSymbols) {
		t.Fatalf("C symbols = %v, want %v", symbols, expectedCSymbols)
	}
	wasm, err := os.ReadFile("wasm_exports.txt")
	if err != nil {
		t.Fatal(err)
	}
	wasmSymbols := strings.Fields(string(wasm))
	if !slices.Equal(wasmSymbols, expectedWASMExports) {
		t.Fatalf("WASM exports = %v, want %v", wasmSymbols, expectedWASMExports)
	}
	wasmWrapper, err := os.ReadFile("wasmlib/main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, symbol := range expectedWASMExports {
		if !strings.Contains(string(wasmWrapper), `"`+symbol+`"`) {
			t.Fatalf("WASM wrapper does not register %s", symbol)
		}
	}
	for _, forbidden := range []string{"terminal", "history", "file", "storage", "json", "base64"} {
		if strings.Contains(strings.ToLower(string(header)), forbidden) || strings.Contains(strings.ToLower(string(wasm)), forbidden) {
			t.Fatalf("binding ABI contains business/encoding term %q", forbidden)
		}
	}
}

func TestBindingCoreDoesNotImportPlatformOrDomainOwners(t *testing.T) {
	forbidden := []string{
		"C", "unsafe", "syscall/js", "encoding/json", "encoding/base64",
		"github.com/lozzow/termx/core", "github.com/lozzow/termx/tui", "github.com/lozzow/termx/cmd/termx",
		"github.com/lozzow/termx/private", "github.com/lozzow/termx/internal/protocol",
		"github.com/lozzow/termx/remote", "github.com/pion/webrtc",
	}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != "." && (entry.Name() == "cabi" || entry.Name() == "wasmlib" || entry.Name() == "managedhost") {
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

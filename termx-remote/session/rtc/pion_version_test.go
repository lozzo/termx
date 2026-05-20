package rtc

import (
	"os/exec"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
)

func TestPionVersionsAtRequiredMinimums(t *testing.T) {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("build info is unavailable")
	}

	requireModuleVersionAtLeast(t, buildInfo, "github.com/pion/webrtc/v4", "v4.2.9")
	requireModuleVersionAtLeast(t, buildInfo, "github.com/pion/ice/v4", "v4.2.1")
}

func requireModuleVersionAtLeast(t *testing.T, buildInfo *debug.BuildInfo, modulePath, minimum string) {
	t.Helper()
	version := moduleVersionFromBuildInfo(buildInfo, modulePath)
	if version == "" {
		version = moduleVersionFromGoList(t, modulePath)
	}
	if compareSemver(version, minimum) < 0 {
		t.Fatalf("%s version %s is below required minimum %s", modulePath, version, minimum)
	}
}

func moduleVersionFromBuildInfo(buildInfo *debug.BuildInfo, modulePath string) string {
	for _, dep := range buildInfo.Deps {
		if dep.Path != modulePath {
			continue
		}
		version := dep.Version
		if dep.Replace != nil {
			version = dep.Replace.Version
		}
		return version
	}
	return ""
}

func moduleVersionFromGoList(t *testing.T, modulePath string) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Version}}", modulePath).Output()
	if err != nil {
		t.Fatalf("query %s version: %v", modulePath, err)
	}
	version := strings.TrimSpace(string(out))
	if version == "" {
		t.Fatalf("module %s version is empty", modulePath)
	}
	return version
}

func compareSemver(a, b string) int {
	av := parseSemver(a)
	bv := parseSemver(b)
	for i := range av {
		if av[i] < bv[i] {
			return -1
		}
		if av[i] > bv[i] {
			return 1
		}
	}
	return 0
}

func parseSemver(version string) [3]int {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	base, _, _ := strings.Cut(version, "-")
	parts := strings.Split(base, ".")
	var out [3]int
	for i := 0; i < len(parts) && i < len(out); i++ {
		value, _ := strconv.Atoi(parts[i])
		out[i] = value
	}
	return out
}

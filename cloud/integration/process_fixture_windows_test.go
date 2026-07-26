//go:build windows

package integration_test

func testIdleTerminalCommand() []string {
	return []string{"cmd.exe", "/d", "/q"}
}

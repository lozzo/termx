package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

func main() {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		fmt.Fprintln(os.Stderr, "keydump requires a TTY on stdin")
		os.Exit(1)
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to enter raw mode: %v\n", err)
		os.Exit(1)
	}
	defer term.Restore(fd, oldState)

	fmt.Println("keydump: press keys, Ctrl-C to exit")

	buf := make([]byte, 1024)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read failed: %v\n", err)
			os.Exit(1)
		}
		data := buf[:n]
		fmt.Printf("len=%d hex=%s quoted=%q\n", len(data), hexBytes(data), data)
		if len(data) == 1 && data[0] == 0x03 {
			return
		}
	}
}

func hexBytes(data []byte) string {
	parts := make([]string, len(data))
	for i, b := range data {
		parts[i] = fmt.Sprintf("%02x", b)
	}
	return strings.Join(parts, " ")
}

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lozzow/termx"
	"github.com/lozzow/termx/protocol"
	unixtransport "github.com/lozzow/termx/transport/unix"
	"github.com/lozzow/termx/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use: "termx",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
				return fmt.Errorf("termx TUI requires an interactive terminal; use `termx --help` or subcommands like `new`, `ls`, `attach`, `kill`, `daemon`")
			}
			client, err := dialOrStartClient(resolveSocket(socket))
			if err != nil {
				return err
			}
			defer client.Close()
			return tui.Run(tui.NewProtocolClient(client), tui.Config{
				DefaultShell: os.Getenv("SHELL"),
				Workspace:    "main",
			}, os.Stdin, os.Stdout)
		},
	}
	cmd.PersistentFlags().StringVar(&socket, "socket", "", "socket path")
	cmd.AddCommand(daemonCommand(&socket))
	cmd.AddCommand(newCommand(&socket))
	cmd.AddCommand(lsCommand(&socket))
	cmd.AddCommand(killCommand(&socket))
	cmd.AddCommand(attachCommand(&socket))
	return cmd
}

func daemonCommand(socket *string) *cobra.Command {
	return &cobra.Command{
		Use: "daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			opts := []termx.ServerOption{}
			if *socket != "" {
				opts = append(opts, termx.WithSocketPath(*socket))
			}
			srv := termx.NewServer(opts...)
			return srv.ListenAndServe(ctx)
		},
	}
}

func newCommand(socket *string) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:  "new -- CMD [ARGS...]",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				shell := os.Getenv("SHELL")
				if shell == "" {
					shell = "/bin/sh"
				}
				args = []string{shell}
			}
			client, err := dialOrStartClient(resolveSocket(*socket))
			if err != nil {
				return err
			}
			defer client.Close()
			created, err := client.Create(context.Background(), protocol.CreateParams{
				Command: args,
				Name:    name,
				Size:    currentSize(),
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), created.TerminalID)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "terminal name")
	return cmd
}

func lsCommand(socket *string) *cobra.Command {
	return &cobra.Command{
		Use: "ls",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := dialOrStartClient(resolveSocket(*socket))
			if err != nil {
				return err
			}
			defer client.Close()
			list, err := client.List(context.Background())
			if err != nil {
				return err
			}
			for _, item := range list.Terminals {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%dx%d\n",
					item.ID, item.Name, strings.Join(item.Command, " "), item.State, item.Size.Cols, item.Size.Rows)
			}
			return nil
		},
	}
}

func killCommand(socket *string) *cobra.Command {
	return &cobra.Command{
		Use:  "kill <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := dialOrStartClient(resolveSocket(*socket))
			if err != nil {
				return err
			}
			defer client.Close()
			return client.Kill(context.Background(), args[0])
		},
	}
}

func attachCommand(socket *string) *cobra.Command {
	return &cobra.Command{
		Use:  "attach <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := dialOrStartClient(resolveSocket(*socket))
			if err != nil {
				return err
			}
			defer client.Close()

			attach, err := client.Attach(context.Background(), args[0], string(termx.ModeCollaborator))
			if err != nil {
				return err
			}
			stream, stop := client.Stream(attach.Channel)
			defer stop()

			snap, err := client.Snapshot(context.Background(), args[0], 0, 200)
			if err == nil {
				renderSnapshot(cmd.OutOrStdout(), snap)
			}

			fd := int(os.Stdin.Fd())
			oldState, err := term.MakeRaw(fd)
			if err == nil {
				defer term.Restore(fd, oldState)
			}

			if size := currentSize(); size.Cols > 0 && size.Rows > 0 {
				_ = client.Resize(context.Background(), attach.Channel, size.Cols, size.Rows)
			}

			errCh := make(chan error, 2)
			go func() {
				buf := make([]byte, 1024)
				var escapes int
				for {
					n, err := os.Stdin.Read(buf)
					if err != nil {
						errCh <- err
						return
					}
					data := buf[:n]
					if len(data) == 1 && data[0] == 0x1c {
						escapes++
						if escapes == 2 {
							errCh <- nil
							return
						}
						continue
					}
					escapes = 0
					if err := client.Input(context.Background(), attach.Channel, data); err != nil {
						errCh <- err
						return
					}
				}
			}()

			for {
				select {
				case err := <-errCh:
					if err == nil || err == io.EOF {
						return nil
					}
					return err
				case msg, ok := <-stream:
					if !ok {
						return nil
					}
					switch msg.Type {
					case protocol.TypeOutput:
						_, _ = cmd.OutOrStdout().Write(msg.Payload)
					case protocol.TypeSyncLost:
						snap, err := client.Snapshot(context.Background(), args[0], 0, 200)
						if err == nil {
							renderSnapshot(cmd.OutOrStdout(), snap)
						}
					case protocol.TypeClosed:
						return nil
					}
				}
			}
		},
	}
}

func dialClient(path string) (*protocol.Client, error) {
	conn, err := unixtransport.Dial(path)
	if err != nil {
		return nil, err
	}
	client := protocol.NewClient(conn)
	if err := client.Hello(context.Background(), protocol.Hello{
		Version: protocol.Version,
		Client:  "termx-cli",
	}); err != nil {
		return nil, err
	}
	return client, nil
}

func dialOrStartClient(path string) (*protocol.Client, error) {
	client, err := dialClient(path)
	if err == nil {
		return client, nil
	}
	if startErr := startDaemon(path); startErr != nil {
		return nil, err
	}
	if waitErr := tui.WaitForSocket(path, 5*time.Second, func() error {
		c, dialErr := dialClient(path)
		if dialErr != nil {
			return dialErr
		}
		_ = c.Close()
		return nil
	}); waitErr != nil {
		return nil, waitErr
	}
	return dialClient(path)
}

func startDaemon(path string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--socket", path, "daemon")
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Start()
}

func resolveSocket(path string) string {
	if path != "" {
		return path
	}
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return runtimeDir + "/termx.sock"
	}
	return fmt.Sprintf("%s/termx-%d.sock", os.TempDir(), os.Getuid())
}

func currentSize() protocol.Size {
	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return protocol.Size{}
	}
	return protocol.Size{Cols: uint16(cols), Rows: uint16(rows)}
}

func renderSnapshot(w io.Writer, snap *protocol.Snapshot) {
	for _, row := range snap.Scrollback {
		fmt.Fprintln(w, rowString(row))
	}
	for _, row := range snap.Screen.Cells {
		fmt.Fprintln(w, rowString(row))
	}
}

func rowString(row []protocol.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return strings.TrimRight(b.String(), " ")
}

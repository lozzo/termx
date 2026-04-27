package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lozzow/termx/remote/agent"
	"github.com/lozzow/termx/transport"
	unixtransport "github.com/lozzow/termx/transport/unix"
	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var listenAddr string
	var socketPath string

	cmd := &cobra.Command{
		Use:   "termx-agent",
		Short: "Expose a minimal local WebRTC direct-offer bridge for a termx daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(listenAddr, socketPath)
		},
	}
	cmd.Flags().StringVar(&listenAddr, "listen", "127.0.0.1:8081", "HTTP listen address for direct local WebRTC offers")
	cmd.Flags().StringVar(&socketPath, "socket", "", "termx daemon socket path")
	return cmd
}

func run(listenAddr, socketPath string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	handler := agent.NewWebRTCHandler(func(context.Context) (transport.Transport, error) {
		return unixtransport.Dial(resolveSocket(socketPath))
	})
	defer handler.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/rtc/offer", agent.NewLocalOfferHandler(handler))
	server := &http.Server{
		Addr:    listenAddr,
		Handler: mux,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
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

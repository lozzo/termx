package webshell

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lozzow/termx"
	"github.com/lozzow/termx/protocol"
	unixtransport "github.com/lozzow/termx/transport/unix"
	"github.com/spf13/cobra"
)

//go:embed web/index.html
var webAssets embed.FS

var webPageTemplate = template.Must(template.ParseFS(webAssets, "web/index.html"))

var webSocketUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			return true
		}
		parsed, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return strings.EqualFold(parsed.Host, r.Host)
	},
}

type webTerminalApp struct {
	socketPath string
	terminalID string
	mode       string
	title      string
	logger     *slog.Logger
}

type webPageData struct {
	TerminalID string
	Mode       string
	Title      string
}

type webServerEvent struct {
	Type         string `json:"type"`
	TerminalID   string `json:"terminal_id,omitempty"`
	Mode         string `json:"mode,omitempty"`
	Title        string `json:"title,omitempty"`
	Cols         uint16 `json:"cols,omitempty"`
	Rows         uint16 `json:"rows,omitempty"`
	DroppedBytes uint64 `json:"dropped_bytes,omitempty"`
	ExitCode     *int   `json:"exit_code,omitempty"`
	Error        string `json:"error,omitempty"`
}

type webClientEvent struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

type webSocketWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

type Dependencies struct {
	OpenLogger        func(path string) (*slog.Logger, func() error, string, error)
	ResolveSocket     func(path string) string
	DialOrStartClient func(path string, logFile string, logger *slog.Logger) (*protocol.Client, error)
	CurrentSize       func() protocol.Size
}

func (d Dependencies) openLogger(path string) (*slog.Logger, func() error, string, error) {
	if d.OpenLogger == nil {
		return nil, nil, "", fmt.Errorf("webshell: OpenLogger dependency is nil")
	}
	return d.OpenLogger(path)
}

func (d Dependencies) resolveSocket(path string) string {
	if d.ResolveSocket == nil {
		return path
	}
	return d.ResolveSocket(path)
}

func (d Dependencies) dialOrStartClient(path string, logFile string, logger *slog.Logger) (*protocol.Client, error) {
	if d.DialOrStartClient == nil {
		return nil, fmt.Errorf("webshell: DialOrStartClient dependency is nil")
	}
	return d.DialOrStartClient(path, logFile, logger)
}

func (d Dependencies) currentSize() protocol.Size {
	if d.CurrentSize == nil {
		return protocol.Size{}
	}
	return d.CurrentSize()
}

func NewCommand(socket *string, logFile *string, deps Dependencies) *cobra.Command {
	var (
		listenAddr string
		terminalID string
		name       string
		mode       string
	)
	cmd := &cobra.Command{
		Use:   "web [--id TERMINAL_ID] [-- CMD [ARGS...]]",
		Short: "Serve a minimal xterm.js client for performance comparison",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, closeLogger, logPath, err := deps.openLogger(*logFile)
			if err != nil {
				return err
			}
			defer closeLogger()

			attachMode, err := parseWebAttachMode(mode)
			if err != nil {
				return err
			}
			createArgs, err := resolveWebCreateArgs(terminalID, args)
			if err != nil {
				return err
			}

			socketPath := deps.resolveSocket(*socket)
			client, err := deps.dialOrStartClient(socketPath, logPath, logger)
			if err != nil {
				return err
			}
			defer client.Close()

			title := terminalID
			if len(createArgs) > 0 {
				title = strings.Join(createArgs, " ")
				if strings.TrimSpace(name) != "" {
					title = strings.TrimSpace(name)
				}
				created, err := client.Create(context.Background(), protocol.CreateParams{
					Command: createArgs,
					Name:    strings.TrimSpace(name),
					Size:    webInitialSize(deps),
				})
				if err != nil {
					return err
				}
				terminalID = created.TerminalID
			} else if terminalID == "" {
				return fmt.Errorf("web terminal id resolved empty")
			}
			if title == "" {
				title = terminalID
			}

			app := &webTerminalApp{
				socketPath: socketPath,
				terminalID: terminalID,
				mode:       attachMode,
				title:      title,
				logger:     logger,
			}

			listener, err := net.Listen("tcp", listenAddr)
			if err != nil {
				return err
			}
			defer listener.Close()

			server := &http.Server{
				Handler: app.routes(),
			}

			errc := make(chan error, 1)
			go func() {
				errc <- server.Serve(listener)
			}()

			url := "http://" + listener.Addr().String()
			fmt.Fprintf(cmd.OutOrStdout(), "termx web ready\nurl\t%s\nterminal\t%s\nmode\t%s\n", url, terminalID, attachMode)
			logger.Info("started web terminal bridge", "url", url, "terminal_id", terminalID, "mode", attachMode)

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			select {
			case <-ctx.Done():
			case serveErr := <-errc:
				if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
					return serveErr
				}
			}

			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			select {
			case serveErr := <-errc:
				if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
					return serveErr
				}
			default:
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&listenAddr, "listen", "127.0.0.1:0", "HTTP listen address")
	cmd.Flags().StringVar(&terminalID, "id", "", "existing terminal id to attach")
	cmd.Flags().StringVar(&name, "name", "", "name used when creating a new terminal")
	cmd.Flags().StringVar(&mode, "mode", string(termx.ModeCollaborator), "attach mode: collaborator or observer")
	return cmd
}

func parseWebAttachMode(mode string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "", string(termx.ModeCollaborator):
		return string(termx.ModeCollaborator), nil
	case string(termx.ModeObserver):
		return string(termx.ModeObserver), nil
	default:
		return "", fmt.Errorf("unsupported web attach mode %q", mode)
	}
}

func resolveWebCreateArgs(terminalID string, args []string) ([]string, error) {
	if strings.TrimSpace(terminalID) != "" {
		if len(args) > 0 {
			return nil, fmt.Errorf("cannot combine --id with a creation command")
		}
		return nil, nil
	}
	if len(args) > 0 {
		return append([]string(nil), args...), nil
	}
	shell := os.Getenv("SHELL")
	if strings.TrimSpace(shell) == "" {
		shell = "/bin/sh"
	}
	return []string{shell}, nil
}

func webInitialSize(deps Dependencies) protocol.Size {
	size := deps.currentSize()
	if size.Cols > 0 && size.Rows > 0 {
		return size
	}
	return protocol.Size{Cols: 120, Rows: 40}
}

func (a *webTerminalApp) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("/ws", a.handleWebSocket)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}

func (a *webTerminalApp) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := webPageTemplate.Execute(w, webPageData{
		TerminalID: a.terminalID,
		Mode:       a.mode,
		Title:      a.title,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *webTerminalApp) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := webSocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	conn.SetReadLimit(1 << 20)
	writer := &webSocketWriter{conn: conn}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	client, err := dialWebProtocolClient(a.socketPath)
	if err != nil {
		_ = writer.writeJSON(webServerEvent{Type: "error", Error: err.Error()})
		return
	}
	defer client.Close()

	attached, err := client.Attach(ctx, a.terminalID, a.mode)
	if err != nil {
		_ = writer.writeJSON(webServerEvent{Type: "error", Error: err.Error()})
		return
	}
	stream, stopStream := client.Stream(attached.Channel)
	defer stopStream()

	if err := writer.writeJSON(webServerEvent{
		Type:       "ready",
		TerminalID: a.terminalID,
		Mode:       attached.Mode,
		Title:      a.title,
	}); err != nil {
		return
	}

	streamErrc := make(chan error, 1)
	go func() {
		streamErrc <- a.forwardProtocolStream(ctx, writer, stream)
	}()

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			cancel()
			select {
			case <-streamErrc:
			default:
			}
			return
		}
		switch messageType {
		case websocket.BinaryMessage:
			if a.mode != string(termx.ModeCollaborator) || len(data) == 0 {
				continue
			}
			if err := client.Input(context.Background(), attached.Channel, data); err != nil {
				_ = writer.writeJSON(webServerEvent{Type: "error", Error: err.Error()})
				return
			}
		case websocket.TextMessage:
			var msg webClientEvent
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = writer.writeJSON(webServerEvent{Type: "error", Error: "invalid control message"})
				continue
			}
			if msg.Type != "resize" {
				continue
			}
			if a.mode != string(termx.ModeCollaborator) || msg.Cols == 0 || msg.Rows == 0 {
				continue
			}
			if err := client.Resize(context.Background(), attached.Channel, msg.Cols, msg.Rows); err != nil {
				_ = writer.writeJSON(webServerEvent{Type: "error", Error: err.Error()})
				return
			}
		}

		select {
		case err := <-streamErrc:
			if err != nil && !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				_ = writer.writeJSON(webServerEvent{Type: "error", Error: err.Error()})
			}
			return
		default:
		}
	}
}

func (a *webTerminalApp) forwardProtocolStream(ctx context.Context, writer *webSocketWriter, stream <-chan protocol.StreamFrame) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case frame, ok := <-stream:
			if !ok {
				return nil
			}
			switch frame.Type {
			case protocol.TypeOutput:
				if len(frame.Payload) == 0 {
					continue
				}
				if err := writer.writeBinary(frame.Payload); err != nil {
					return err
				}
			case protocol.TypeResize:
				cols, rows, err := protocol.DecodeResizePayload(frame.Payload)
				if err != nil {
					return err
				}
				if err := writer.writeJSON(webServerEvent{Type: "resize", Cols: cols, Rows: rows}); err != nil {
					return err
				}
			case protocol.TypeBootstrapDone:
				if err := writer.writeJSON(webServerEvent{Type: "bootstrap_done"}); err != nil {
					return err
				}
			case protocol.TypeSyncLost:
				dropped, err := protocol.DecodeSyncLostPayload(frame.Payload)
				if err != nil {
					return err
				}
				if err := writer.writeJSON(webServerEvent{Type: "sync_lost", DroppedBytes: dropped}); err != nil {
					return err
				}
			case protocol.TypeClosed:
				code, err := protocol.DecodeClosedPayload(frame.Payload)
				if err != nil {
					return err
				}
				exitCode := code
				if err := writer.writeJSON(webServerEvent{Type: "closed", ExitCode: &exitCode}); err != nil {
					return err
				}
				return nil
			}
		}
	}
}

func dialWebProtocolClient(path string) (*protocol.Client, error) {
	conn, err := unixtransport.Dial(path)
	if err != nil {
		return nil, err
	}
	client := protocol.NewClient(conn)
	if err := client.Hello(context.Background(), protocol.Hello{
		Version: protocol.Version,
		Client:  "termx-web",
	}); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func (w *webSocketWriter) writeJSON(msg webServerEvent) error {
	if w == nil || w.conn == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteJSON(msg)
}

func (w *webSocketWriter) writeBinary(data []byte) error {
	if w == nil || w.conn == nil || len(data) == 0 {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(websocket.BinaryMessage, data)
}

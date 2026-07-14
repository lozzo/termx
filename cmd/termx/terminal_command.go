package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/spf13/cobra"
)

const localEndpointID = "local"

type terminalProtocolClient interface {
	Create(context.Context, protocol.CreateParams) (*protocol.CreateResult, error)
	List(context.Context) (*protocol.ListResult, error)
	Restart(context.Context, string) error
	Kill(context.Context, string) error
	Remove(context.Context, string) error
	SetTags(context.Context, string, map[string]string) error
	SetMetadata(context.Context, string, string, map[string]string) error
	Close() error
}

type terminalCommandRuntime struct {
	socket     *string
	logFile    *string
	configPath *string
}

type terminalView struct {
	Target     string            `json:"target"`
	EndpointID string            `json:"endpoint_id"`
	TerminalID string            `json:"terminal_id"`
	Name       string            `json:"name"`
	Command    []string          `json:"command"`
	CWD        string            `json:"cwd,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
	State      string            `json:"state"`
	Cols       uint16            `json:"cols"`
	Rows       uint16            `json:"rows"`
	CreatedAt  string            `json:"created_at,omitempty"`
	ExitCode   *int              `json:"exit_code,omitempty"`
	ExitedAt   string            `json:"exited_at,omitempty"`
}

type terminalListEnvelope struct {
	SchemaVersion int            `json:"schema_version"`
	Kind          string         `json:"kind"`
	Items         []terminalView `json:"items"`
}

type terminalItemEnvelope struct {
	SchemaVersion int          `json:"schema_version"`
	Kind          string       `json:"kind"`
	Item          terminalView `json:"item"`
}

type commandResultEnvelope struct {
	SchemaVersion int    `json:"schema_version"`
	Kind          string `json:"kind"`
	Target        string `json:"target"`
	State         string `json:"state,omitempty"`
}

type cliError struct {
	code    int
	message string
	cause   error
}

func (err *cliError) Error() string {
	if err == nil {
		return "command failed"
	}
	return err.message
}

func (err *cliError) Unwrap() error { return err.cause }

func newTerminalCommand(runtime terminalCommandRuntime) *cobra.Command {
	command := &cobra.Command{
		Use:   "terminal",
		Short: "Manage terminals owned by a daemon endpoint",
		Long:  "Manage terminal lifecycle through the owning daemon. Local is the only endpoint supported until CLI004.",
	}
	command.AddCommand(
		newTerminalCreateCommand(runtime, "create"),
		newTerminalListCommand(runtime, "list"),
		newTerminalShowCommand(runtime),
		newTerminalAttachCommand(runtime, "attach"),
		newTerminalMutationCommand(runtime, "restart", "Restart a terminal from its daemon-owned process specification", terminalRestart),
		newTerminalMutationCommand(runtime, "kill", "Stop a terminal process and preserve its record and history", terminalKill),
		newTerminalMutationCommand(runtime, "remove", "Remove an exited terminal record", terminalRemove),
		newTerminalRenameCommand(runtime),
		newTerminalTagCommand(runtime),
	)
	return command
}

func newTerminalAliasCommands(runtime terminalCommandRuntime) []*cobra.Command {
	return []*cobra.Command{
		newTerminalCreateCommand(runtime, "new"),
		newTerminalListCommand(runtime, "ls"),
		newTerminalAttachCommand(runtime, "attach"),
		newTerminalMutationCommand(runtime, "kill", "Stop a terminal process and preserve its record and history", terminalKill),
		newTerminalMutationCommand(runtime, "rm", "Remove an exited terminal record", terminalRemove),
	}
}

func (runtime terminalCommandRuntime) open(cmd *cobra.Command) (terminalProtocolClient, func(), error) {
	// CLI002 只解析 local target；terminal lifecycle 与 metadata truth 始终来自这个 daemon client。
	logger, closeLogger, logPath, err := openLogFileLogger(*runtime.logFile)
	if err != nil {
		return nil, func() {}, err
	}
	client, err := dialOrStartV3Client(resolveV3Socket(*runtime.socket), logPath, logger)
	if err != nil {
		closeLogger()
		return nil, func() {}, classifyCLIError(err)
	}
	cmd.Root().SilenceUsage = true
	return client, func() {
		_ = client.Close()
		closeLogger()
	}, nil
}

func newTerminalCreateCommand(runtime terminalCommandRuntime, use string) *cobra.Command {
	var name, cwd string
	var environment []string
	var tags map[string]string
	var cols, rows uint16
	var jsonOutput, attach bool
	command := &cobra.Command{
		Use:   use + " [-- COMMAND...]",
		Short: "Create a terminal on the local daemon",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if (cols == 0) != (rows == 0) {
				return usageCLIError("--cols and --rows must be provided together")
			}
			if len(args) == 0 {
				shell := strings.TrimSpace(os.Getenv("SHELL"))
				if shell == "" {
					shell = "/bin/sh"
				}
				args = []string{shell}
			}
			client, closeClient, err := runtime.open(cmd)
			if err != nil {
				return err
			}
			defer closeClient()
			size := protocol.Size{Cols: cols, Rows: rows}
			if cols == 0 {
				size = currentSize()
			}
			terminalID := newV3TerminalID()
			if strings.TrimSpace(name) != "" {
				terminalID = strings.TrimSpace(name)
			}
			created, err := client.Create(cmd.Context(), protocol.CreateParams{
				ID: terminalID, Name: strings.TrimSpace(name), Command: append([]string(nil), args...),
				Dir: strings.TrimSpace(cwd), Env: append([]string(nil), environment...), Tags: cloneTerminalTags(tags), Size: size,
			})
			if err != nil {
				return classifyCLIError(err)
			}
			target := localTerminalTarget(created.TerminalID)
			if jsonOutput {
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(commandResultEnvelope{SchemaVersion: 1, Kind: "terminal_created", Target: target, State: created.State}); err != nil {
					return err
				}
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), target)
			}
			if attach {
				return runLocalAttachCommand(cmd, created.TerminalID, *runtime.socket, *runtime.logFile, *runtime.configPath)
			}
			return nil
		},
	}
	command.Flags().StringVar(&name, "name", "", "stable daemon-local terminal name")
	command.Flags().StringVar(&cwd, "cwd", "", "working directory on the daemon host")
	command.Flags().StringArrayVar(&environment, "env", nil, "environment entry KEY=VALUE (repeatable)")
	command.Flags().StringToStringVar(&tags, "tag", nil, "terminal tag KEY=VALUE (repeatable)")
	command.Flags().Uint16Var(&cols, "cols", 0, "initial terminal columns")
	command.Flags().Uint16Var(&rows, "rows", 0, "initial terminal rows")
	command.Flags().BoolVar(&attach, "attach", false, "attach after creation")
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func newTerminalListCommand(runtime terminalCommandRuntime, use string) *cobra.Command {
	var jsonOutput, noHeader bool
	var stateFilter string
	var tagFilters []string
	command := &cobra.Command{
		Use:   use,
		Short: "List terminals from the local daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, closeClient, err := runtime.open(cmd)
			if err != nil {
				return err
			}
			defer closeClient()
			result, err := client.List(cmd.Context())
			if err != nil {
				return classifyCLIError(err)
			}
			views := make([]terminalView, 0, len(result.Terminals))
			for _, item := range result.Terminals {
				if stateFilter != "" && item.State != stateFilter || !terminalMatchesTags(item, tagFilters) {
					continue
				}
				views = append(views, terminalInfoView(item))
			}
			sort.Slice(views, func(i, j int) bool { return views[i].Target < views[j].Target })
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(terminalListEnvelope{SchemaVersion: 1, Kind: "terminal_list", Items: views})
			}
			return writeTerminalTable(cmd.OutOrStdout(), views, noHeader)
		},
	}
	command.Flags().StringVar(&stateFilter, "state", "", "filter by exact lifecycle state")
	command.Flags().StringArrayVar(&tagFilters, "tag", nil, "filter by KEY or KEY=VALUE (repeatable)")
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	command.Flags().BoolVar(&noHeader, "no-header", false, "omit the human table header")
	return command
}

func newTerminalShowCommand(runtime terminalCommandRuntime) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "show TARGET",
		Short: "Show one terminal from the local daemon",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, closeClient, err := runtime.open(cmd)
			if err != nil {
				return err
			}
			defer closeClient()
			item, err := findLocalTerminal(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			view := terminalInfoView(item)
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(terminalItemEnvelope{SchemaVersion: 1, Kind: "terminal", Item: view})
			}
			return writeTerminalDetail(cmd.OutOrStdout(), view)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func newTerminalAttachCommand(runtime terminalCommandRuntime, use string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " TARGET",
		Short: "Attach the TUI to a terminal",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			terminalID, err := localTerminalID(args[0])
			if err != nil {
				return err
			}
			cmd.Root().SilenceUsage = true
			return runLocalAttachCommand(cmd, terminalID, *runtime.socket, *runtime.logFile, *runtime.configPath)
		},
	}
}

type terminalMutation int

const (
	terminalRestart terminalMutation = iota + 1
	terminalKill
	terminalRemove
)

func newTerminalMutationCommand(runtime terminalCommandRuntime, use, short string, mutation terminalMutation) *cobra.Command {
	var jsonOutput, quiet bool
	command := &cobra.Command{
		Use:   use + " TARGET",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, closeClient, err := runtime.open(cmd)
			if err != nil {
				return err
			}
			defer closeClient()
			item, err := findLocalTerminal(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			switch mutation {
			case terminalRestart:
				err = client.Restart(cmd.Context(), item.ID)
			case terminalKill:
				err = client.Kill(cmd.Context(), item.ID)
			case terminalRemove:
				if item.State == "running" {
					return &cliError{code: 4, message: fmt.Sprintf("terminal %s is running; kill it before remove", localTerminalTarget(item.ID))}
				}
				err = client.Remove(cmd.Context(), item.ID)
			}
			if err != nil {
				return classifyCLIError(err)
			}
			if quiet {
				return nil
			}
			kind := "terminal_restarted"
			if mutation == terminalKill {
				kind = "terminal_killed"
			} else if mutation == terminalRemove {
				kind = "terminal_removed"
			}
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(commandResultEnvelope{SchemaVersion: 1, Kind: kind, Target: localTerminalTarget(item.ID)})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", localTerminalTarget(item.ID), strings.TrimPrefix(kind, "terminal_"))
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	command.Flags().BoolVarP(&quiet, "quiet", "q", false, "suppress success output")
	return command
}

func newTerminalRenameCommand(runtime terminalCommandRuntime) *cobra.Command {
	return &cobra.Command{
		Use:   "rename TARGET NAME",
		Short: "Rename a terminal without changing its stable ID",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(args[1]) == "" {
				return usageCLIError("terminal name cannot be empty")
			}
			client, closeClient, err := runtime.open(cmd)
			if err != nil {
				return err
			}
			defer closeClient()
			item, err := findLocalTerminal(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			if err := client.SetMetadata(cmd.Context(), item.ID, strings.TrimSpace(args[1]), item.Tags); err != nil {
				return classifyCLIError(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\trenamed\n", localTerminalTarget(item.ID))
			return nil
		},
	}
}

func newTerminalTagCommand(runtime terminalCommandRuntime) *cobra.Command {
	var removeKeys []string
	command := &cobra.Command{
		Use:   "tag TARGET [KEY=VALUE...]",
		Short: "Set or remove terminal tags",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && len(removeKeys) == 0 {
				return usageCLIError("provide at least one KEY=VALUE or --remove KEY")
			}
			updates, err := parseTerminalTags(args[1:])
			if err != nil {
				return err
			}
			client, closeClient, err := runtime.open(cmd)
			if err != nil {
				return err
			}
			defer closeClient()
			item, err := findLocalTerminal(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			tags := cloneTerminalTags(item.Tags)
			if tags == nil {
				tags = make(map[string]string)
			}
			for key, value := range updates {
				tags[key] = value
			}
			for _, key := range removeKeys {
				key = strings.TrimSpace(key)
				if key == "" {
					return usageCLIError("tag remove key cannot be empty")
				}
				delete(tags, key)
			}
			if err := client.SetTags(cmd.Context(), item.ID, tags); err != nil {
				return classifyCLIError(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\ttagged\n", localTerminalTarget(item.ID))
			return nil
		},
	}
	command.Flags().StringArrayVar(&removeKeys, "remove", nil, "remove a tag key (repeatable)")
	return command
}

func findLocalTerminal(ctx context.Context, client terminalProtocolClient, target string) (protocol.TerminalInfo, error) {
	// show/mutation 复用 daemon list projection 查找，CLI 不建立第二份 terminal inventory。
	id, err := localTerminalID(target)
	if err != nil {
		return protocol.TerminalInfo{}, err
	}
	result, err := client.List(ctx)
	if err != nil {
		return protocol.TerminalInfo{}, classifyCLIError(err)
	}
	for _, item := range result.Terminals {
		if item.ID == id {
			return item, nil
		}
	}
	return protocol.TerminalInfo{}, &cliError{code: 3, message: fmt.Sprintf("terminal %s was not found", localTerminalTarget(id))}
}

func localTerminalID(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", usageCLIError("terminal target cannot be empty")
	}
	if endpoint, id, found := strings.Cut(target, ":"); found {
		if endpoint != localEndpointID || id == "" || strings.Contains(id, ":") {
			return "", usageCLIError("CLI002 only accepts local:TERMINAL_ID targets")
		}
		return id, nil
	}
	return target, nil
}

func terminalInfoView(item protocol.TerminalInfo) terminalView {
	return terminalView{
		Target: localTerminalTarget(item.ID), EndpointID: localEndpointID, TerminalID: item.ID,
		Name: item.Name, Command: append([]string(nil), item.Command...), CWD: item.CWD,
		Tags: cloneTerminalTags(item.Tags), State: item.State, Cols: item.Size.Cols, Rows: item.Size.Rows,
		CreatedAt: formatTerminalTime(item.CreatedAt), ExitCode: item.ExitCode, ExitedAt: formatTerminalTime(item.ExitedAt),
	}
}

func localTerminalTarget(id string) string { return localEndpointID + ":" + id }

func formatTerminalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func writeTerminalTable(writer io.Writer, views []terminalView, noHeader bool) error {
	if !noHeader {
		if _, err := fmt.Fprintln(writer, "TARGET\tNAME\tSTATE\tSIZE\tCOMMAND"); err != nil {
			return err
		}
	}
	for _, view := range views {
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%dx%d\t%s\n", view.Target, view.Name, view.State, view.Cols, view.Rows, strings.Join(view.Command, " ")); err != nil {
			return err
		}
	}
	return nil
}

func writeTerminalDetail(writer io.Writer, view terminalView) error {
	_, err := fmt.Fprintf(writer, "Target: %s\nName: %s\nState: %s\nCommand: %s\nCWD: %s\nSize: %dx%d\n", view.Target, view.Name, view.State, strings.Join(view.Command, " "), view.CWD, view.Cols, view.Rows)
	return err
}

func terminalMatchesTags(item protocol.TerminalInfo, filters []string) bool {
	for _, filter := range filters {
		key, value, hasValue := strings.Cut(filter, "=")
		actual, exists := item.Tags[strings.TrimSpace(key)]
		if !exists || hasValue && actual != value {
			return false
		}
	}
	return true
}

func parseTerminalTags(values []string) (map[string]string, error) {
	tags := make(map[string]string, len(values))
	for _, value := range values {
		key, tagValue, found := strings.Cut(value, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			return nil, usageCLIError("tags must use KEY=VALUE")
		}
		tags[key] = tagValue
	}
	return tags, nil
}

func cloneTerminalTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for key, value := range tags {
		out[key] = value
	}
	return out
}

func usageCLIError(message string) error { return &cliError{code: 2, message: message} }

func classifyCLIError(err error) error {
	if err == nil {
		return nil
	}
	var classified *cliError
	if errors.As(err, &classified) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &cliError{code: 7, message: err.Error(), cause: err}
	}
	var requestError *protocol.RequestError
	if errors.As(err, &requestError) {
		// 退出码只读取 daemon 的 typed protocol code，禁止根据可变错误文案猜测领域状态。
		code := 1
		switch requestError.Code {
		case 400:
			code = 4
		case 401, 403:
			code = 5
		case 404:
			code = 3
		case 503:
			code = 6
		}
		return &cliError{code: code, message: requestError.Message, cause: err}
	}
	return &cliError{code: 6, message: err.Error(), cause: err}
}

func cliExitCode(err error) int {
	if err == nil {
		return 0
	}
	var classified *cliError
	if errors.As(err, &classified) {
		return classified.code
	}
	return 1
}

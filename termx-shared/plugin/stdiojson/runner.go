// Package stdiojson 提供 one-shot external plugin runner 的 host-side 执行器。
package stdiojson

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"time"

	"github.com/lozzow/termx/termx-shared/plugin"
)

const (
	defaultStdoutLimit = int64(1024 * 1024)
	defaultStderrLimit = int64(64 * 1024)
	defaultWaitDelay   = 2 * time.Second
)

// RunnerConfig 描述 stdio JSON runner 的 host-side 启动参数。
// Spec 必须来自已解析的 manifest/catalog；Env 和 Dir 由 host policy 控制，不能由插件 response 动态改写。
type RunnerConfig struct {
	Spec        plugin.RunnerSpec
	Env         []string
	Dir         string
	Timeout     time.Duration
	WaitDelay   time.Duration
	StdoutLimit int64
	StderrLimit int64
}

// Runner 是 one-shot stdio JSON external runner。
// 它只负责启动进程、传输一个 request、读取一个 response；action dispatch、capability 校验和 trace 派生属于上层 host。
type Runner struct {
	command     []string
	env         []string
	dir         string
	timeout     time.Duration
	waitDelay   time.Duration
	stdoutLimit int64
	stderrLimit int64
}

// RunReport 描述一次外部 runner 进程执行的宿主侧观测结果。
// 它用于审计和测试，不作为插件可回写的业务结果，也不表示 action call 已经执行成功。
type RunReport struct {
	ExitCode        int
	Stderr          string
	StdoutTruncated bool
	StderrTruncated bool
	TimedOut        bool
}

// ErrCommandFailed 表示 runner 进程没有以 0 状态退出。
// 调用方可结合 RunReport 判断 stderr、exit code 或 timeout，不能把 stdout 中的内容当作可信响应。
var ErrCommandFailed = errors.New("stdio_json runner command failed")

// ErrOutputLimitExceeded 表示 runner stdout 或 stderr 超过 host 允许的输出上限。
// host 已经取消该 one-shot 进程；调用方不能继续消费 stdout 中可能截断的响应。
var ErrOutputLimitExceeded = errors.New("stdio_json runner output limit exceeded")

// NewRunner 创建 stdio JSON one-shot runner。
// 这里只接受 RunnerStdioJSON 和显式 argv，避免通过 shell 字符串解释插件命令。
func NewRunner(config RunnerConfig) (*Runner, error) {
	if config.Spec.Type != plugin.RunnerStdioJSON {
		return nil, fmt.Errorf("stdio_json runner requires stdio_json spec")
	}
	if len(config.Spec.Command) == 0 || config.Spec.Command[0] == "" {
		return nil, fmt.Errorf("stdio_json runner command is required")
	}
	stdoutLimit := config.StdoutLimit
	if stdoutLimit <= 0 {
		stdoutLimit = defaultStdoutLimit
	}
	stderrLimit := config.StderrLimit
	if stderrLimit <= 0 {
		stderrLimit = defaultStderrLimit
	}
	waitDelay := config.WaitDelay
	if waitDelay <= 0 {
		waitDelay = defaultWaitDelay
	}
	return &Runner{
		command:     append([]string(nil), config.Spec.Command...),
		env:         append([]string(nil), config.Env...),
		dir:         config.Dir,
		timeout:     config.Timeout,
		waitDelay:   waitDelay,
		stdoutLimit: stdoutLimit,
		stderrLimit: stderrLimit,
	}, nil
}

// Run 启动外部进程并执行一个 stdio JSON request。
// request 会先被 host-side 校验；response 使用严格 JSON 解码，未知字段会失败，防止 runner 输出 SourcePluginID/trace 等伪造字段被静默忽略。
func (runner *Runner) Run(ctx context.Context, request plugin.StdioJSONRequest) (plugin.StdioJSONResponse, RunReport, error) {
	if runner == nil {
		return plugin.StdioJSONResponse{}, RunReport{}, fmt.Errorf("stdio_json runner is nil")
	}
	request = plugin.NormalizeStdioJSONRequest(request)
	if err := plugin.ValidateStdioJSONRequest(request); err != nil {
		return plugin.StdioJSONResponse{}, RunReport{}, err
	}

	runCtx, cancel := runner.contextForRequest(ctx, request)
	defer cancel()

	cmd := exec.CommandContext(runCtx, runner.command[0], runner.command[1:]...)
	cmd.Dir = runner.dir
	cmd.Env = runner.environment(request)
	cmd.WaitDelay = runner.waitDelay

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return plugin.StdioJSONResponse{}, RunReport{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return plugin.StdioJSONResponse{}, RunReport{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return plugin.StdioJSONResponse{}, RunReport{}, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return plugin.StdioJSONResponse{}, RunReport{}, err
	}

	stdoutCh := readCappedAsync(stdout, runner.stdoutLimit, cancel)
	stderrCh := readCappedAsync(stderr, runner.stderrLimit, cancel)

	encodeErr := json.NewEncoder(stdin).Encode(request)
	closeErr := stdin.Close()
	waitErr := cmd.Wait()
	stdoutCapture := <-stdoutCh
	stderrCapture := <-stderrCh

	report := RunReport{
		ExitCode:        exitCode(waitErr),
		Stderr:          string(stderrCapture.Data),
		StdoutTruncated: stdoutCapture.Truncated,
		StderrTruncated: stderrCapture.Truncated,
		TimedOut:        errors.Is(runCtx.Err(), context.DeadlineExceeded),
	}
	if encodeErr != nil {
		return plugin.StdioJSONResponse{}, report, encodeErr
	}
	if closeErr != nil {
		return plugin.StdioJSONResponse{}, report, closeErr
	}
	if stdoutCapture.Truncated || stderrCapture.Truncated {
		return plugin.StdioJSONResponse{}, report, ErrOutputLimitExceeded
	}
	if waitErr != nil {
		if runCtx.Err() != nil {
			return plugin.StdioJSONResponse{}, report, fmt.Errorf("%w: %w: %w", ErrCommandFailed, waitErr, runCtx.Err())
		}
		return plugin.StdioJSONResponse{}, report, fmt.Errorf("%w: %w", ErrCommandFailed, waitErr)
	}

	response, err := decodeResponse(stdoutCapture.Data)
	if err != nil {
		return plugin.StdioJSONResponse{}, report, err
	}
	if err := plugin.ValidateStdioJSONResponse(response, request.RequestID, request.DeadlineUnixNS); err != nil {
		return plugin.StdioJSONResponse{}, report, err
	}
	return response.Clone(), report, nil
}

func (runner *Runner) contextForRequest(ctx context.Context, request plugin.StdioJSONRequest) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx := ctx
	cancelAll := func() {}
	if runner.timeout > 0 {
		next, cancel := context.WithTimeout(runCtx, runner.timeout)
		runCtx = next
		previous := cancelAll
		cancelAll = func() {
			cancel()
			previous()
		}
	}
	if request.DeadlineUnixNS > 0 {
		next, cancel := context.WithDeadline(runCtx, time.Unix(0, request.DeadlineUnixNS))
		runCtx = next
		previous := cancelAll
		cancelAll = func() {
			cancel()
			previous()
		}
	}
	return runCtx, cancelAll
}

func (runner *Runner) environment(request plugin.StdioJSONRequest) []string {
	env := append([]string(nil), runner.env...)
	env = append(env,
		"TERMX_PLUGIN_ID="+string(request.PluginID),
		"TERMX_PLUGIN_HOST="+string(request.Host),
		"TERMX_PLUGIN_REQUEST_ID="+request.RequestID,
		"TERMX_PLUGIN_INVOCATION_KIND="+string(request.Kind),
		"TERMX_PLUGIN_HANDLER="+request.Handler,
		"TERMX_PLUGIN_DEADLINE_UNIX_NS="+strconv.FormatInt(request.DeadlineUnixNS, 10),
	)
	context := request.Context
	if context.ClientKind != "" {
		env = append(env, "TERMX_CLIENT_KIND="+string(context.ClientKind))
	}
	if context.ClientSessionID != "" {
		env = append(env, "TERMX_CLIENT_SESSION_ID="+context.ClientSessionID)
	}
	if context.WorkspaceID != "" {
		env = append(env, "TERMX_WORKSPACE_ID="+context.WorkspaceID)
	}
	if context.EndpointID != "" {
		env = append(env, "TERMX_DAEMON_ENDPOINT="+string(context.EndpointID))
	}
	if context.TerminalRef != nil {
		env = append(env,
			"TERMX_TERMINAL_ENDPOINT="+string(context.TerminalRef.EndpointID),
			"TERMX_TERMINAL_ID="+string(context.TerminalRef.TerminalID),
		)
	}
	if context.DaemonID != "" {
		env = append(env, "TERMX_DAEMON_ID="+context.DaemonID)
	}
	if context.DaemonTerminalID != "" {
		env = append(env, "TERMX_DAEMON_TERMINAL_ID="+string(context.DaemonTerminalID))
	}
	if context.GrantRef != "" {
		env = append(env, "TERMX_PLUGIN_GRANT_REF="+context.GrantRef)
	}
	return env
}

func decodeResponse(data []byte) (plugin.StdioJSONResponse, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var response plugin.StdioJSONResponse
	if err := decoder.Decode(&response); err != nil {
		return plugin.StdioJSONResponse{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return plugin.StdioJSONResponse{}, fmt.Errorf("stdio_json runner emitted multiple json values")
	}
	return response, nil
}

type cappedCapture struct {
	Data      []byte
	Truncated bool
}

func readCappedAsync(reader io.Reader, limit int64, onTruncate func()) <-chan cappedCapture {
	ch := make(chan cappedCapture, 1)
	go func() {
		capture := &cappedWriter{limit: limit, onTruncate: onTruncate}
		_, _ = io.Copy(capture, reader)
		ch <- cappedCapture{Data: capture.buf.Bytes(), Truncated: capture.truncated}
	}()
	return ch
}

type cappedWriter struct {
	limit      int64
	buf        bytes.Buffer
	truncated  bool
	onTruncate func()
}

func (writer *cappedWriter) Write(p []byte) (int, error) {
	if writer.limit <= 0 {
		return len(p), nil
	}
	remaining := writer.limit - int64(writer.buf.Len())
	if remaining > 0 {
		n := len(p)
		if int64(n) > remaining {
			n = int(remaining)
		}
		_, _ = writer.buf.Write(p[:n])
	}
	if int64(len(p)) > remaining {
		if !writer.truncated && writer.onTruncate != nil {
			writer.onTruncate()
		}
		writer.truncated = true
	}
	return len(p), nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

package stdiojson

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-shared/plugin"
)

func TestRunnerExecutesOneShotProcessAndReturnsTypedResponse(t *testing.T) {
	runner := newHelperRunner(t, "echo", RunnerConfig{})
	request := validActionRequest()
	response, report, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("run helper: %v report=%#v", err, report)
	}
	if report.ExitCode != 0 || report.TimedOut {
		t.Fatalf("unexpected report %#v", report)
	}

	var result map[string]string
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result["plugin_id"] != "acme.deploy" || result["host_env"] != string(plugin.HostOneShot) {
		t.Fatalf("runner should receive host-derived env and envelope, got %#v", result)
	}
	if result["plugin_env"] != "acme.deploy" || result["kind_env"] != string(plugin.StdioJSONInvocationAction) || result["handler_env"] != "run" {
		t.Fatalf("runner should receive termx invocation env, got %#v", result)
	}
	if result["client_kind_env"] != string(plugin.ClientKindTUI) || result["session_env"] != "tui-1" || result["workspace_env"] != "default" {
		t.Fatalf("runner should receive client context env, got %#v", result)
	}
	if result["endpoint_env"] != "remote-a" || result["terminal_endpoint_env"] != "remote-a" || result["terminal_id_env"] != "codex" || result["grant_ref_env"] != "grant:acme" {
		t.Fatalf("runner should receive terminal/grant context env, got %#v", result)
	}
	if result["home_env"] != "" {
		t.Fatalf("runner should not inherit host HOME by default, got %#v", result)
	}
	if result["terminal_id"] != "codex" || result["request_id_env"] != request.RequestID || result["deadline_env"] == "" {
		t.Fatalf("runner should receive context env, got %#v", result)
	}
	if len(response.ActionCalls) != 1 {
		t.Fatalf("expected helper action call, got %#v", response.ActionCalls)
	}
	call := response.ActionCalls[0]
	if call.ActionID != "termx.client.panel.close" || call.Target.TerminalRef == nil || call.Target.TerminalRef.EndpointID != "remote-a" {
		t.Fatalf("unexpected action call %#v", call)
	}
}

func TestRunnerRejectsUnknownResponseFieldsThatCouldForgeIdentity(t *testing.T) {
	runner := newHelperRunner(t, "forged", RunnerConfig{})
	_, _, err := runner.Run(context.Background(), validActionRequest())
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("response with forged source field should fail, got %v", err)
	}
}

func TestRunnerRejectsNonZeroExitAndCapturesStderr(t *testing.T) {
	runner := newHelperRunner(t, "fail", RunnerConfig{})
	_, report, err := runner.Run(context.Background(), validActionRequest())
	if !errors.Is(err, ErrCommandFailed) {
		t.Fatalf("expected command failure, got %v", err)
	}
	if report.ExitCode != 7 {
		t.Fatalf("expected exit code 7, got %#v", report)
	}
	if !strings.Contains(report.Stderr, "intentional failure") {
		t.Fatalf("stderr should be captured, got %#v", report)
	}
}

func TestRunnerTimeoutKillsOneShotProcess(t *testing.T) {
	runner := newHelperRunner(t, "sleep", RunnerConfig{Timeout: 20 * time.Millisecond})
	_, report, err := runner.Run(context.Background(), validActionRequest())
	if !errors.Is(err, ErrCommandFailed) {
		t.Fatalf("expected command failure on timeout, got %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout should preserve deadline cause, got %v", err)
	}
	if !report.TimedOut {
		t.Fatalf("expected timeout report, got %#v", report)
	}
}

func TestRunnerParentCancelIsNotReportedAsTimeout(t *testing.T) {
	runner := newHelperRunner(t, "sleep", RunnerConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, report, err := runner.Run(ctx, validActionRequest())
	if !errors.Is(err, ErrCommandFailed) || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected command failure with cancel cause, got %v", err)
	}
	if report.TimedOut {
		t.Fatalf("parent cancel should not be reported as timeout, got %#v", report)
	}
}

func TestRunnerRejectsMultipleJSONValues(t *testing.T) {
	runner := newHelperRunner(t, "multiple", RunnerConfig{})
	_, _, err := runner.Run(context.Background(), validActionRequest())
	if err == nil || !strings.Contains(err.Error(), "multiple json values") {
		t.Fatalf("expected multiple JSON rejection, got %v", err)
	}
}

func TestRunnerCancelsOnOutputLimit(t *testing.T) {
	runner := newHelperRunner(t, "stdout_limit", RunnerConfig{StdoutLimit: 16, WaitDelay: 20 * time.Millisecond})
	_, report, err := runner.Run(context.Background(), validActionRequest())
	if !errors.Is(err, ErrOutputLimitExceeded) {
		t.Fatalf("expected output limit error, got %v report=%#v", err, report)
	}
	if !report.StdoutTruncated {
		t.Fatalf("expected stdout truncation report, got %#v", report)
	}
}

func TestRunnerWaitDelayUnblocksInheritedPipes(t *testing.T) {
	runner := newHelperRunner(t, "leakpipe", RunnerConfig{WaitDelay: 20 * time.Millisecond})
	start := time.Now()
	response, _, err := runner.Run(context.Background(), validActionRequest())
	if err != nil && (!errors.Is(err, ErrCommandFailed) || !errors.Is(err, exec.ErrWaitDelay)) {
		t.Fatalf("expected success or wait delay command failure, got %v", err)
	}
	if err == nil && response.Status != plugin.StdioJSONStatusOK {
		t.Fatalf("expected ok response when wait delay is not needed, got %#v", response)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("runner should not block on inherited pipes, elapsed=%s", elapsed)
	}
}

func TestRunnerValidatesRequestBeforeStartingProcess(t *testing.T) {
	runner := newHelperRunner(t, "echo", RunnerConfig{})
	request := validActionRequest()
	request.DeadlineUnixNS = 0
	if _, _, err := runner.Run(context.Background(), request); err == nil {
		t.Fatalf("invalid request should fail before process start")
	}
}

func TestRunnerRejectsInvalidSpec(t *testing.T) {
	if _, err := NewRunner(RunnerConfig{Spec: plugin.RunnerSpec{Type: plugin.RunnerBuiltin, Command: []string{"x"}}}); err == nil {
		t.Fatalf("builtin spec should not create stdio_json runner")
	}
	if _, err := NewRunner(RunnerConfig{Spec: plugin.RunnerSpec{Type: plugin.RunnerStdioJSON}}); err == nil {
		t.Fatalf("empty command should fail")
	}
}

func newHelperRunner(t *testing.T, mode string, config RunnerConfig) *Runner {
	t.Helper()
	config.Spec = plugin.RunnerSpec{
		Type:    plugin.RunnerStdioJSON,
		Command: []string{os.Args[0], "-test.run=TestStdioJSONHelperProcess", "--"},
	}
	config.Env = append(config.Env,
		"TERMX_STDIOJSON_HELPER=1",
		"TERMX_STDIOJSON_HELPER_MODE="+mode,
	)
	runner, err := NewRunner(config)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	return runner
}

func validActionRequest() plugin.StdioJSONRequest {
	return plugin.NormalizeStdioJSONRequest(plugin.StdioJSONRequest{
		RequestID:      "req-stdio",
		PluginID:       "acme.deploy",
		Host:           plugin.HostOneShot,
		Handler:        "run",
		Kind:           plugin.StdioJSONInvocationAction,
		TraceParent:    plugin.TraceParent{TraceID: "trace-stdio", Token: "token-stdio"},
		DeadlineUnixNS: time.Now().Add(time.Minute).UnixNano(),
		Context: plugin.StdioJSONContext{
			ClientKind:      plugin.ClientKindTUI,
			ClientSessionID: "tui-1",
			WorkspaceID:     "default",
			EndpointID:      "remote-a",
			TerminalRef:     &plugin.TerminalRef{EndpointID: "remote-a", TerminalID: "codex"},
			GrantRef:        "grant:acme",
		},
		Action: &plugin.StdioJSONActionInvocation{
			ActionID: "acme.deploy.run",
			Params:   json.RawMessage(`{"value":1}`),
			Target: plugin.ActionTarget{
				SessionID:   "tui-1",
				ActivePanel: true,
				TerminalRef: &plugin.TerminalRef{EndpointID: "remote-a", TerminalID: "codex"},
			},
		},
	})
}

func TestStdioJSONHelperProcess(t *testing.T) {
	if os.Getenv("TERMX_STDIOJSON_LEAK_CHILD") == "1" {
		time.Sleep(200 * time.Millisecond)
		os.Exit(0)
	}
	if os.Getenv("TERMX_STDIOJSON_HELPER") != "1" {
		return
	}
	mode := os.Getenv("TERMX_STDIOJSON_HELPER_MODE")
	switch mode {
	case "echo":
		runEchoHelper()
	case "forged":
		runForgedHelper()
	case "fail":
		fmt.Fprintln(os.Stderr, "intentional failure")
		os.Exit(7)
	case "sleep":
		time.Sleep(2 * time.Second)
		os.Exit(0)
	case "multiple":
		runMultipleJSONHelper()
	case "stdout_limit":
		fmt.Fprint(os.Stdout, strings.Repeat("x", 4096))
	case "leakpipe":
		runLeakPipeHelper()
	default:
		fmt.Fprintf(os.Stderr, "unknown helper mode %s\n", mode)
		os.Exit(2)
	}
	os.Exit(0)
}

func runEchoHelper() {
	var request plugin.StdioJSONRequest
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		fmt.Fprintf(os.Stderr, "decode request: %v\n", err)
		os.Exit(2)
	}
	terminalID := ""
	if request.Context.TerminalRef != nil {
		terminalID = string(request.Context.TerminalRef.TerminalID)
	}
	result, _ := json.Marshal(map[string]string{
		"plugin_id":             string(request.PluginID),
		"host_env":              os.Getenv("TERMX_PLUGIN_HOST"),
		"plugin_env":            os.Getenv("TERMX_PLUGIN_ID"),
		"kind_env":              os.Getenv("TERMX_PLUGIN_INVOCATION_KIND"),
		"handler_env":           os.Getenv("TERMX_PLUGIN_HANDLER"),
		"request_id_env":        os.Getenv("TERMX_PLUGIN_REQUEST_ID"),
		"deadline_env":          os.Getenv("TERMX_PLUGIN_DEADLINE_UNIX_NS"),
		"terminal_id":           terminalID,
		"client_kind_env":       os.Getenv("TERMX_CLIENT_KIND"),
		"session_env":           os.Getenv("TERMX_CLIENT_SESSION_ID"),
		"workspace_env":         os.Getenv("TERMX_WORKSPACE_ID"),
		"endpoint_env":          os.Getenv("TERMX_DAEMON_ENDPOINT"),
		"terminal_endpoint_env": os.Getenv("TERMX_TERMINAL_ENDPOINT"),
		"terminal_id_env":       os.Getenv("TERMX_TERMINAL_ID"),
		"grant_ref_env":         os.Getenv("TERMX_PLUGIN_GRANT_REF"),
		"home_env":              os.Getenv("HOME"),
	})
	response := plugin.StdioJSONResponse{
		Protocol:  plugin.StdioJSONProtocol,
		RequestID: request.RequestID,
		Status:    plugin.StdioJSONStatusOK,
		Result:    result,
		ActionCalls: []plugin.StdioJSONActionCall{
			{
				ActionID:       "termx.client.panel.close",
				Target:         plugin.ActionTarget{TerminalRef: &plugin.TerminalRef{EndpointID: "remote-a", TerminalID: "codex"}},
				DeadlineUnixNS: request.DeadlineUnixNS,
			},
		},
	}
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		fmt.Fprintf(os.Stderr, "encode response: %v\n", err)
		os.Exit(2)
	}
}

func runForgedHelper() {
	var request plugin.StdioJSONRequest
	_ = json.NewDecoder(os.Stdin).Decode(&request)
	fmt.Fprintf(os.Stdout, `{"protocol":%q,"request_id":%q,"status":"ok","source_plugin_id":"evil"}`+"\n", plugin.StdioJSONProtocol, request.RequestID)
}

func runMultipleJSONHelper() {
	var request plugin.StdioJSONRequest
	_ = json.NewDecoder(os.Stdin).Decode(&request)
	response := plugin.StdioJSONResponse{
		Protocol:  plugin.StdioJSONProtocol,
		RequestID: request.RequestID,
		Status:    plugin.StdioJSONStatusOK,
	}
	_ = json.NewEncoder(os.Stdout).Encode(response)
	_ = json.NewEncoder(os.Stdout).Encode(response)
}

func runLeakPipeHelper() {
	var request plugin.StdioJSONRequest
	_ = json.NewDecoder(os.Stdin).Decode(&request)
	cmd := exec.Command(os.Args[0], "-test.run=TestStdioJSONHelperProcess", "--")
	cmd.Env = append(os.Environ(), "TERMX_STDIOJSON_LEAK_CHILD=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start leak child: %v\n", err)
		os.Exit(2)
	}
	response := plugin.StdioJSONResponse{
		Protocol:  plugin.StdioJSONProtocol,
		RequestID: request.RequestID,
		Status:    plugin.StdioJSONStatusOK,
	}
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		fmt.Fprintf(os.Stderr, "encode leak response: %v\n", err)
		os.Exit(2)
	}
}

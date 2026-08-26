package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/qf-studio/pilot/internal/logging"
)

// OMPError reports a failed Oh My Pi RPC execution using the common backend
// error contract so the runner can apply its existing retry policy.
type OMPError struct {
	Type    string
	Message string
	Stderr  string
}

func (e *OMPError) Error() string        { return fmt.Sprintf("%s: %s", e.Type, e.Message) }
func (e *OMPError) ErrorType() string    { return e.Type }
func (e *OMPError) ErrorMessage() string { return e.Message }
func (e *OMPError) ErrorStderr() string  { return e.Stderr }

// OMPBackend executes one task through OMP's documented JSONL RPC protocol.
// A process is intentionally scoped to an execution attempt: this keeps agent
// state isolated across tickets while allowing the task's own turn to retain a
// durable OMP session for diagnostics and manual continuation.
type OMPBackend struct {
	config           *OMPConfig
	heartbeatTimeout time.Duration
	log              *slog.Logger
}

func NewOMPBackend(config *OMPConfig) *OMPBackend {
	if config == nil {
		config = &OMPConfig{}
	}
	if config.Command == "" {
		config.Command = "omp"
	}
	if config.Version == "" {
		config.Version = "18.0.5"
	}
	return &OMPBackend{
		config:           config,
		heartbeatTimeout: DefaultHeartbeatTimeout,
		log:              logging.WithComponent("executor.omp"),
	}
}

func (b *OMPBackend) Name() string { return BackendTypeOMP }

func (b *OMPBackend) SetHeartbeatTimeout(timeout time.Duration) {
	b.heartbeatTimeout = timeout
}

func (b *OMPBackend) IsAvailable() bool {
	path, err := exec.LookPath(b.config.Command)
	if err != nil {
		return false
	}
	if b.config.Version == "" {
		return true
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.TrimSpace(string(out)), b.config.Version)
}

func (b *OMPBackend) Execute(ctx context.Context, opts ExecuteOptions) (*BackendResult, error) {
	args := []string{"--mode", "rpc", "--yolo", "--no-title"}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if thinking := b.resolveThinking(opts.Effort); thinking != "" {
		args = append(args, "--thinking", thinking)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	args = append(args, b.config.ExtraArgs...)

	profileDir, agentDir, err := ompProfilePaths(b.config.ProfileDir)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, b.config.Command, args...)
	cmd.Dir = opts.ProjectPath
	configureProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd, syscall.SIGKILL) }
	cmd.Env = append(os.Environ(), "PILOT_EXECUTOR=1")
	if profileDir != "" {
		cmd.Env = append(cmd.Env,
			"PI_CONFIG_DIR="+profileDir,
			"PI_CODING_AGENT_DIR="+agentDir,
		)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create OMP stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create OMP stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("create OMP stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start OMP: %w", err)
	}

	frames := make(chan []byte, 32)
	readErr := make(chan error, 1)
	go readOMPFrames(stdout, frames, readErr)
	var stderrBuffer bytes.Buffer
	var stderrWG sync.WaitGroup
	stderrWG.Add(1)
	go func() {
		defer stderrWG.Done()
		_, _ = io.Copy(&stderrBuffer, io.LimitReader(stderrPipe, MaxStderrBufferBytes))
	}()

	result := &BackendResult{}
	if err := b.awaitReady(ctx, frames, stdin); err != nil {
		_ = stdin.Close()
		_ = killProcessGroup(cmd, syscall.SIGKILL)
		_ = cmd.Wait()
		stderrWG.Wait()
		return result, &OMPError{Type: "invalid_config", Message: err.Error(), Stderr: stderrBuffer.String()}
	}
	if err := writeOMPFrame(stdin, map[string]any{
		"id": "pilot-tools", "type": "set_host_tools", "tools": ompHostTools(opts.HostToolHandler != nil),
	}); err != nil {
		return b.finishWithProcessError(cmd, stdin, stderrWG, &stderrBuffer, result, err)
	}
	if err := writeOMPFrame(stdin, map[string]any{"id": "pilot-prompt", "type": "prompt", "message": opts.Prompt}); err != nil {
		return b.finishWithProcessError(cmd, stdin, stderrWG, &stderrBuffer, result, err)
	}

	lastEvent := time.Now()
	heartbeatTimeout := b.heartbeatTimeout
	if opts.LivenessPolicy.HeartbeatFloor > heartbeatTimeout {
		heartbeatTimeout = opts.LivenessPolicy.HeartbeatFloor
	}
	ticker := time.NewTicker(HeartbeatCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = writeOMPFrame(stdin, map[string]any{"type": "abort"})
			_ = stdin.Close()
			_ = killProcessGroup(cmd, syscall.SIGKILL)
			return b.finishWithProcessError(cmd, nil, stderrWG, &stderrBuffer, result, ctx.Err())
		case <-ticker.C:
			if age := time.Since(lastEvent); age > heartbeatTimeout {
				if opts.HeartbeatCallback != nil {
					opts.HeartbeatCallback(cmd.Process.Pid, age)
				}
				_ = stdin.Close()
				_ = killProcessGroup(cmd, syscall.SIGKILL)
				return b.finishWithProcessError(cmd, nil, stderrWG, &stderrBuffer, result, fmt.Errorf("OMP heartbeat timed out after %s", age.Round(time.Second)))
			}
		case err := <-readErr:
			if err != nil {
				return b.finishWithProcessError(cmd, stdin, stderrWG, &stderrBuffer, result, err)
			}
			readErr = nil
		case frame, ok := <-frames:
			if !ok {
				return b.finishWithProcessError(cmd, stdin, stderrWG, &stderrBuffer, result, fmt.Errorf("OMP RPC stream closed before terminal agent_end"))
			}
			lastEvent = time.Now()
			completed, frameErr := b.handleFrame(ctx, stdin, frame, result, opts)
			if frameErr != nil {
				return b.finishWithProcessError(cmd, stdin, stderrWG, &stderrBuffer, result, frameErr)
			}
			if completed {
				_ = stdin.Close()
				waitErr := cmd.Wait()
				stderrWG.Wait()
				result.Stderr = stderrBuffer.String()
				result.Model = opts.Model
				if waitErr != nil {
					return result, &OMPError{Type: "api_error", Message: waitErr.Error(), Stderr: result.Stderr}
				}
				result.Success = true
				result.SawSuccessResult = true
				return result, nil
			}
		}
	}
}

// ompProfilePaths resolves the configured OMP config root and its shared agent
// session directory. Environment variable values are not shell-expanded, so
// a configured ~/.omp must become an absolute path before launching OMP.
func ompProfilePaths(profileDir string) (string, string, error) {
	profileDir = strings.TrimSpace(profileDir)
	if profileDir == "" {
		return "", "", nil
	}
	if profileDir == "~" || strings.HasPrefix(profileDir, "~/") || strings.HasPrefix(profileDir, `~\`) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", "", fmt.Errorf("resolve OMP profile home directory: %w", err)
		}
		profileDir = filepath.Join(homeDir, profileDir[1:])
	}
	profileDir, err := filepath.Abs(profileDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve OMP profile directory: %w", err)
	}
	return filepath.Clean(profileDir), filepath.Join(profileDir, "agent"), nil
}

// RunOMPQuery executes a stateless, non-interactive query for executor
// sidecars such as classification and review. It deliberately shares the RPC
// transport with task execution so no direct provider client or legacy CLI can
// bypass the OMP profile.
func RunOMPQuery(ctx context.Context, config *OMPConfig, projectPath, prompt, model string) (string, error) {
	if projectPath == "" {
		var err error
		projectPath, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve OMP query working directory: %w", err)
		}
	}
	queryConfig := OMPConfig{}
	if config != nil {
		queryConfig = *config
		queryConfig.ExtraArgs = append([]string(nil), config.ExtraArgs...)
	}
	queryConfig.ExtraArgs = append(queryConfig.ExtraArgs, "--no-session")
	result, err := NewOMPBackend(&queryConfig).Execute(ctx, ExecuteOptions{
		Prompt:      prompt,
		ProjectPath: projectPath,
		Model:       model,
	})
	if err != nil {
		return "", err
	}
	return result.Output, nil
}

func ompConfigFromBackend(config *BackendConfig) *OMPConfig {
	if config == nil {
		return nil
	}
	return config.OMP
}

// runOMPCLICompat keeps legacy sidecar test seams intact while routing their
// old print-style arguments through the OMP RPC transport. It understands only
// prompt/model flags because those are the sole parts of the old invocation
// contract that affect model behavior.
func runOMPCLICompat(ctx context.Context, args ...string) ([]byte, error) {
	return runOMPCLICompatWithConfig(ctx, nil, args...)
}

func runOMPCLICompatWithConfig(ctx context.Context, config *OMPConfig, args ...string) ([]byte, error) {
	var prompt, model string
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "-p", "--prompt":
			if index+1 < len(args) {
				index++
				prompt = args[index]
			}
		case "--model":
			if index+1 < len(args) {
				index++
				model = args[index]
			}
		}
	}
	if prompt == "" {
		return nil, fmt.Errorf("OMP query requires a prompt")
	}
	output, err := RunOMPQuery(ctx, config, "", prompt, model)
	return []byte(output), err
}

func (b *OMPBackend) awaitReady(ctx context.Context, frames <-chan []byte, stdin io.Writer) error {
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("OMP did not emit a ready frame within 30s")
		case frame, ok := <-frames:
			if !ok {
				return fmt.Errorf("OMP exited before emitting a ready frame")
			}
			var message struct {
				Type                      string `json:"type"`
				SupportedProtocolVersions []int  `json:"supportedProtocolVersions"`
			}
			if err := json.Unmarshal(frame, &message); err != nil {
				return fmt.Errorf("decode OMP startup frame: %w", err)
			}
			if message.Type != "ready" {
				continue
			}
			for _, version := range message.SupportedProtocolVersions {
				if version == 2 {
					return writeOMPFrame(stdin, map[string]any{"id": "pilot-protocol", "type": "negotiate_protocol", "protocolVersion": 2})
				}
			}
			return nil
		}
	}
}

func (b *OMPBackend) handleFrame(ctx context.Context, stdin io.Writer, frame []byte, result *BackendResult, opts ExecuteOptions) (bool, error) {
	var message map[string]any
	if err := json.Unmarshal(frame, &message); err != nil {
		return false, fmt.Errorf("decode OMP RPC frame: %w", err)
	}
	raw := string(frame)
	typeName, _ := message["type"].(string)
	switch typeName {
	case "message_update":
		if event, ok := message["assistantMessageEvent"].(map[string]any); ok {
			if eventType, _ := event["type"].(string); eventType == "text_delta" {
				if delta, _ := event["delta"].(string); delta != "" {
					result.LastAssistantText += delta
					b.emit(opts, BackendEvent{Type: EventTypeStreamDelta, Raw: raw, Message: delta})
				}
			}
		}
	case "tool_execution_start":
		toolName := ompToolName(message)
		b.emit(opts, BackendEvent{Type: EventTypeToolUse, Raw: raw, ToolName: toolName, Message: "Using " + toolName})
	case "tool_execution_end":
		b.emit(opts, BackendEvent{Type: EventTypeToolResult, Raw: raw, ToolResult: ompEventText(message)})
	case "host_tool_call":
		return false, b.respondHostTool(ctx, stdin, message, opts.HostToolHandler)
	case "response":
		if success, ok := message["success"].(bool); ok && !success {
			return false, fmt.Errorf("OMP %s failed: %v", message["command"], message["error"])
		}
	case "agent_end":
		terminal, hasTerminal := message["isTerminal"].(bool)
		if hasTerminal && !terminal {
			return false, nil
		}
		stopReason, errorMessage := ompAgentEndStatus(message)
		if stopReason == "error" || stopReason == "aborted" {
			if errorMessage == "" {
				errorMessage = "agent ended without a completion message"
			}
			return false, fmt.Errorf("OMP agent %s: %s", stopReason, errorMessage)
		}
		if result.LastAssistantText == "" {
			result.LastAssistantText = ompAssistantText(message)
		}
		result.Output = result.LastAssistantText
		b.emit(opts, BackendEvent{Type: EventTypeResult, Raw: raw, Message: result.Output})
		return true, nil
	}
	return false, nil
}

// ompAgentEndStatus extracts the terminal assistant status from an agent_end
// frame. OMP represents provider failures as a terminal assistant message with
// stopReason "error", rather than as an RPC-level response error.
func ompAgentEndStatus(message map[string]any) (string, string) {
	stopReason, _ := message["stopReason"].(string)
	errorMessage, _ := message["errorMessage"].(string)
	messages, _ := message["messages"].([]any)
	for index := len(messages) - 1; index >= 0; index-- {
		assistant, _ := messages[index].(map[string]any)
		if assistant["role"] != "assistant" {
			continue
		}
		if value, ok := assistant["stopReason"].(string); ok && value != "" {
			stopReason = value
		}
		if value, ok := assistant["errorMessage"].(string); ok && value != "" {
			errorMessage = value
		}
		break
	}
	return strings.TrimSpace(stopReason), strings.TrimSpace(errorMessage)
}

func (b *OMPBackend) respondHostTool(ctx context.Context, stdin io.Writer, request map[string]any, handler OMPHostToolHandler) error {
	id, _ := request["id"].(string)
	toolName, _ := request["toolName"].(string)
	if handler != nil {
		result, err := handler(ctx, toolName, ompHostToolArguments(request))
		if err == nil {
			return writeOMPFrame(stdin, map[string]any{
				"type": "host_tool_result", "id": id,
				"result": map[string]any{"content": []map[string]string{{"type": "text", "text": result}}},
			})
		}
		return writeOMPFrame(stdin, map[string]any{
			"type": "host_tool_result", "id": id, "isError": true,
			"result": map[string]any{"content": []map[string]string{{"type": "text", "text": err.Error()}}},
		})
	}
	return writeOMPFrame(stdin, map[string]any{
		"type": "host_tool_result", "id": id, "isError": true,
		"result": map[string]any{"content": []map[string]string{{"type": "text", "text": fmt.Sprintf("%s is unavailable for this OMP invocation", toolName)}}},
	})
}

func (b *OMPBackend) finishWithProcessError(cmd *exec.Cmd, stdin io.Closer, stderrWG sync.WaitGroup, stderr *bytes.Buffer, result *BackendResult, cause error) (*BackendResult, error) {
	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd.Process != nil {
		_ = killProcessGroup(cmd, syscall.SIGKILL)
	}
	_ = cmd.Wait()
	stderrDone := make(chan struct{})
	go func() {
		stderrWG.Wait()
		close(stderrDone)
	}()
	stderrText := ""
	select {
	case <-stderrDone:
		stderrText = stderr.String()
	case <-time.After(time.Second):
		b.log.Warn("OMP stderr pipe did not close after process shutdown")
	}
	result.Success = false
	result.Stderr = stderrText
	result.Error = cause.Error()
	return result, &OMPError{Type: "api_error", Message: cause.Error(), Stderr: result.Stderr}
}

func (b *OMPBackend) emit(opts ExecuteOptions, event BackendEvent) {
	if opts.EventHandler != nil {
		opts.EventHandler(event)
	}
}

func (b *OMPBackend) resolveThinking(effort string) string {
	if effort == "" {
		return b.config.Thinking
	}
	switch effort {
	case "max":
		return "xhigh"
	case "low", "medium", "high", "xhigh", "minimal", "off":
		return effort
	default:
		return b.config.Thinking
	}
}

func ompHostTools(enabled bool) []map[string]any {
	if !enabled {
		return []map[string]any{}
	}
	return []map[string]any{
		{"name": "pilot_run_quality_gate", "label": "Run Pilot quality gates", "description": "Run Pilot-managed checks for the current task.", "parameters": map[string]any{"type": "object", "additionalProperties": false}},
		{"name": "pilot_github", "label": "Pilot GitHub", "description": "Perform a task-scoped GitHub operation through Pilot policy.", "parameters": map[string]any{"type": "object", "properties": map[string]any{"operation": map[string]any{"type": "string"}}, "required": []string{"operation"}, "additionalProperties": false}},
	}
}

func ompHostToolArguments(request map[string]any) map[string]any {
	for _, key := range []string{"arguments", "input", "params"} {
		if arguments, ok := request[key].(map[string]any); ok {
			return arguments
		}
	}
	return map[string]any{}
}

func writeOMPFrame(writer io.Writer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = writer.Write(append(encoded, '\n'))
	return err
}

func readOMPFrames(reader io.Reader, frames chan<- []byte, errCh chan<- error) {
	defer close(frames)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 2<<20)
	var chunks *ompChunkAccumulator
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if decoded, ok, err := decodeOMPChunk(line, &chunks); err != nil {
			errCh <- err
			return
		} else if ok {
			frames <- decoded
		} else {
			frames <- line
		}
	}
	if chunks != nil {
		errCh <- fmt.Errorf("OMP RPC stream ended before receiving all rpc_chunk frames")
		return
	}
	errCh <- scanner.Err()
}

type ompChunkAccumulator struct {
	count  int
	next   int
	length int
	data   bytes.Buffer
}

// decodeOMPChunk reassembles OMP protocol-v2 rpc_chunk frames. It validates
// ordering and declared payload length so malformed data cannot be mistaken
// for an agent response.
func decodeOMPChunk(line []byte, accumulator **ompChunkAccumulator) ([]byte, bool, error) {
	var chunk struct {
		Type   string `json:"type"`
		Index  int    `json:"index"`
		Count  int    `json:"count"`
		Length int    `json:"length"`
		Data   string `json:"data"`
	}
	if err := json.Unmarshal(line, &chunk); err != nil || chunk.Type != "rpc_chunk" {
		if *accumulator != nil {
			return nil, false, fmt.Errorf("OMP emitted a non-chunk frame before completing rpc_chunk payload")
		}
		return nil, false, nil
	}
	if chunk.Count <= 0 || chunk.Index < 0 || chunk.Index >= chunk.Count || chunk.Length < 0 || chunk.Length > 64<<20 {
		return nil, false, fmt.Errorf("invalid OMP rpc_chunk metadata")
	}
	decoded, err := base64.StdEncoding.DecodeString(chunk.Data)
	if err != nil {
		return nil, false, fmt.Errorf("decode OMP rpc_chunk: %w", err)
	}
	if *accumulator == nil {
		if chunk.Index != 0 {
			return nil, false, fmt.Errorf("OMP rpc_chunk sequence begins at index %d", chunk.Index)
		}
		*accumulator = &ompChunkAccumulator{count: chunk.Count, length: chunk.Length}
	}
	state := *accumulator
	if state.count != chunk.Count || state.length != chunk.Length || state.next != chunk.Index {
		return nil, false, fmt.Errorf("inconsistent OMP rpc_chunk sequence")
	}
	if state.data.Len()+len(decoded) > state.length {
		return nil, false, fmt.Errorf("OMP rpc_chunk payload exceeds declared length")
	}
	_, _ = state.data.Write(decoded)
	state.next++
	if state.next != state.count {
		return nil, false, nil
	}
	if state.data.Len() != state.length {
		return nil, false, fmt.Errorf("OMP rpc_chunk payload length mismatch")
	}
	complete := append([]byte(nil), state.data.Bytes()...)
	*accumulator = nil
	return complete, true, nil
}

func ompToolName(message map[string]any) string {
	for _, key := range []string{"toolName", "tool", "name"} {
		if value, _ := message[key].(string); value != "" {
			return value
		}
	}
	return "tool"
}

func ompEventText(message map[string]any) string {
	for _, key := range []string{"output", "result", "error", "message"} {
		if value, _ := message[key].(string); value != "" {
			return value
		}
	}
	return ""
}

func ompAssistantText(message map[string]any) string {
	messages, _ := message["messages"].([]any)
	for index := len(messages) - 1; index >= 0; index-- {
		entry, _ := messages[index].(map[string]any)
		if entry["role"] != "assistant" {
			continue
		}
		if text := ompContentText(entry["content"]); text != "" {
			return text
		}
	}
	return ""
}

func ompContentText(content any) string {
	blocks, _ := content.([]any)
	var text strings.Builder
	for _, raw := range blocks {
		block, _ := raw.(map[string]any)
		if block["type"] == "text" {
			if value, _ := block["text"].(string); value != "" {
				text.WriteString(value)
			}
		}
	}
	return text.String()
}

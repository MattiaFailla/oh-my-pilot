package executor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOMPBackendExecuteNegotiatesRPCAndServesHostTools(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	command := filepath.Join(directory, "omp")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "omp 18.0.5"
  exit 0
fi
printf '%s\n' '{"type":"ready","supportedProtocolVersions":[2]}'
read protocol
read tools
read prompt
printf '%s\n' '{"type":"host_tool_call","id":"quality-1","toolName":"pilot_run_quality_gate","input":{}}'
read tool_result
printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"completed"}}'
printf '%s\n' '{"type":"agent_end","isTerminal":true}'
`
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake omp: %v", err)
	}

	backend := NewOMPBackend(&OMPConfig{Command: command, Version: "18.0.5"})
	if !backend.IsAvailable() {
		t.Fatal("expected fake OMP to be available")
	}
	called := false
	result, err := backend.Execute(context.Background(), ExecuteOptions{
		Prompt:      "implement this",
		ProjectPath: directory,
		HostToolHandler: func(_ context.Context, name string, arguments map[string]any) (string, error) {
			called = name == "pilot_run_quality_gate" && len(arguments) == 0
			return "quality passed", nil
		},
	})
	if err != nil {
		t.Fatalf("execute OMP: %v", err)
	}
	if !called {
		t.Fatal("expected OMP host tool to run")
	}
	if !result.Success || result.Output != "completed" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestRunOMPQueryUsesConfiguredCommandAndProfile(t *testing.T) {
	directory := t.TempDir()
	command := filepath.Join(directory, "omp")
	marker := filepath.Join(directory, "profile-marker")
	profileDir := filepath.Join(directory, "profile")
	t.Setenv("PROFILE_MARKER", marker)
	script := `#!/bin/sh
printf '%s' "$PI_CODING_AGENT_DIR" > "$PROFILE_MARKER"
printf '%s\n' '{"type":"ready","supportedProtocolVersions":[2]}'
read protocol
read tools
read prompt
printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"classified"}}'
printf '%s\n' '{"type":"agent_end","isTerminal":true}'
`
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake omp: %v", err)
	}

	output, err := RunOMPQuery(context.Background(), &OMPConfig{
		Command:    command,
		ProfileDir: profileDir,
	}, directory, "classify this task", "")
	if err != nil {
		t.Fatalf("run OMP query: %v", err)
	}
	if output != "classified" {
		t.Errorf("output = %q, want classified", output)
	}
	profile, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read profile marker: %v", err)
	}
	if string(profile) != profileDir {
		t.Errorf("PI_CODING_AGENT_DIR = %q, want %q", profile, profileDir)
	}
}

func TestDecodeOMPChunkReassemblesProtocolV2Payload(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"type":"agent_end","isTerminal":true}`)
	first := payload[:15]
	second := payload[15:]
	var state *ompChunkAccumulator

	encode := func(index int, data []byte) []byte {
		frame, err := json.Marshal(map[string]any{
			"type": "rpc_chunk", "index": index, "count": 2,
			"length": len(payload), "data": base64.StdEncoding.EncodeToString(data),
		})
		if err != nil {
			t.Fatalf("encode chunk: %v", err)
		}
		return frame
	}

	decoded, complete, err := decodeOMPChunk(encode(0, first), &state)
	if err != nil || complete || decoded != nil {
		t.Fatalf("unexpected first chunk result: decoded=%q complete=%v err=%v", decoded, complete, err)
	}
	decoded, complete, err = decodeOMPChunk(encode(1, second), &state)
	if err != nil || !complete || string(decoded) != string(payload) {
		t.Fatalf("unexpected completed chunk result: decoded=%q complete=%v err=%v", decoded, complete, err)
	}
}

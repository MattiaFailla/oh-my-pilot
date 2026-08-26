package executor

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// GH-3569/GH-3570 incident: getPostExecutionSummary spawned the claude CLI
// without cmd.Dir, so its git commands ran in the daemon's CWD and reported the
// wrong repo's HEAD as the commit SHA — turning real worker commits into
// ghost-guard no-ops (TASK-320) or recording wrong-repo SHAs as completed
// (TASK-355). The fake claude script below echoes its own working directory as
// the commit_sha, proving the subprocess runs in the directory the caller pins.
func TestGetPostExecutionSummary_RunsInGivenDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake command not portable to windows")
	}

	dir := t.TempDir()
	scriptDir := t.TempDir()
	script := filepath.Join(scriptDir, "fake-claude")
	// Emit an OMP RPC response with the CWD as commit_sha.
	content := `#!/bin/sh
printf '%s\n' '{"type":"ready","supportedProtocolVersions":[2]}'
read protocol
read tools
read prompt
printf '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"{\\"branch_name\\":\\"b\\",\\"commit_sha\\":\\"%s\\",\\"files_changed\\":[],\\"summary\\":\\"s\\"}"}}\n' "$(pwd)"
printf '%s\n' '{"type":"agent_end","isTerminal":true}'
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	r := NewRunner()
	r.config = &BackendConfig{
		Type: BackendTypeOMP,
		OMP:  &OMPConfig{Command: script},
	}

	summary, err := r.getPostExecutionSummary(context.Background(), dir)
	if err != nil {
		t.Fatalf("getPostExecutionSummary: %v", err)
	}
	// macOS TempDir paths may resolve through /private symlinks — compare resolved paths.
	wantDir, _ := filepath.EvalSymlinks(dir)
	gotDir, _ := filepath.EvalSymlinks(summary.CommitSHA)
	if gotDir != wantDir {
		t.Errorf("summary subprocess ran in %q, want pinned dir %q", summary.CommitSHA, dir)
	}
}

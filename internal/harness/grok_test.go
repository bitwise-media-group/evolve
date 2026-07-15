// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/model"
)

// grokJSONSuccess mirrors the headless --output-format json envelope documented
// in Grok's headless-mode guide (uncached input_tokens, separate cache reads).
const grokJSONSuccess = `{
  "text": "Done.",
  "stopReason": "EndTurn",
  "sessionId": "s1",
  "num_turns": 3,
  "usage": {
    "input_tokens": 100,
    "cache_read_input_tokens": 50,
    "output_tokens": 30,
    "total_tokens": 180
  },
  "total_cost_usd": 0.0123
}`

const grokJSONError = `{"type":"error","message":"Couldn't start session: auth failed"}`

func TestGrokParseEvalOutput(t *testing.T) {
	g := NewGrok()
	text, usage := g.ParseEvalOutput([]byte(grokJSONSuccess))
	if text != "Done." {
		t.Errorf("text = %q, want %q", text, "Done.")
	}
	if usage == nil {
		t.Fatal("usage = nil, want populated")
	}
	if got := derefInt(usage.InputTokens); got != 100 {
		t.Errorf("InputTokens = %d, want 100", got)
	}
	if got := derefInt(usage.CacheReadTokens); got != 50 {
		t.Errorf("CacheReadTokens = %d, want 50", got)
	}
	if got := derefInt(usage.OutputTokens); got != 30 {
		t.Errorf("OutputTokens = %d, want 30", got)
	}
	if usage.CostUSD == nil || *usage.CostUSD != 0.0123 {
		t.Errorf("CostUSD = %v, want 0.0123", usage.CostUSD)
	}

	raw := "plain text answer\n"
	if text, usage := g.ParseEvalOutput([]byte(raw)); text != raw || usage != nil {
		t.Errorf("ParseEvalOutput(plain) = (%q, %v), want (%q, nil)", text, usage, raw)
	}
}

func TestGrokRuntimeError(t *testing.T) {
	g := NewGrok()
	tests := []struct {
		name     string
		stdout   string
		exitCode int
		want     string
	}{
		{"gradable result", grokJSONSuccess, 0, ""},
		{"empty", "", 1, "empty CLI output"},
		{"plain text clean exit", "hello\n", 0, ""},
		{"plain text crash", "boom\n", 1, "unparseable CLI output"},
		{"error envelope", grokJSONError, 1, "grok run error: Couldn't start session: auth failed"},
	}
	for _, tt := range tests {
		if got := g.RuntimeError([]byte(tt.stdout), tt.exitCode, false); got != tt.want {
			t.Errorf("%s: RuntimeError = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestGrokScanLine(t *testing.T) {
	g := NewGrok()
	if hit, _ := g.ScanLine([]byte(`{"type":"text","data":"reading skills/my-skill/SKILL.md"}`), "my-skill", ""); !hit {
		t.Error("ScanLine(skill path) = false, want true")
	}
	if hit, _ := g.ScanLine([]byte(`{"type":"text","data":"unrelated"}`), "my-skill", ""); hit {
		t.Error("ScanLine(unrelated) = true, want false")
	}
	// end without a resolvable session is no-hit (stdout carries no tool events).
	if hit, _ := g.ScanLine([]byte(`{"type":"end","sessionId":"missing-session","stopReason":"EndTurn"}`), "my-skill", ""); hit {
		t.Error("ScanLine(end, missing session) = true, want false")
	}
}

// TestGrokScanLineSessionTranscript covers the real trigger path: streaming-json
// never emits tool events, so activation is recovered from the session's ACP
// updates.jsonl once the terminal end event arrives with a sessionId.
func TestGrokScanLineSessionTranscript(t *testing.T) {
	ws := t.TempDir()
	home := grokHomeDir(ws)

	// Synthetic id — must not collide with real ~/.grok sessions on the host.
	const sessionID = "00000000-test-sess-isolated-home-01"
	// Encoded-cwd segment is opaque; any single path component works for the glob.
	sessionDir := filepath.Join(home, "sessions", "%2Ftmp%2Fws", sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Fixture mirrors a real grok updates.jsonl tool_call that Read the skill.
	updates := strings.Join([]string{
		`{"method":"session/update","params":{"update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"use go-project"}}}}`,
		`{"method":"session/update","params":{"update":{"sessionUpdate":"tool_call","toolCallId":"c1","title":"read_file","rawInput":{"target_file":"/tmp/ws/.grok/skills/go-project/SKILL.md"}}}}`,
		`{"method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"c1","status":"completed"}}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(sessionDir, "updates.jsonl"), []byte(updates), 0o644); err != nil {
		t.Fatal(err)
	}

	g := NewGrok()
	end := []byte(`{"type":"end","stopReason":"EndTurn","sessionId":"` + sessionID + `"}`)
	if hit, _ := g.ScanLine(end, "go-project", ws); !hit {
		t.Error("ScanLine(end, session with skill tool_call) = false, want true")
	}
	// A different skill name must not match the go-project path.
	if hit, _ := g.ScanLine(end, "go-docs", ws); hit {
		t.Error("ScanLine(end, other skill) = true, want false")
	}
	// Without workDir the isolated home is invisible (and we clear GROK_HOME).
	t.Setenv("GROK_HOME", "")
	if hit, _ := g.ScanLine(end, "go-project", ""); hit {
		t.Error("ScanLine(end, no workDir) = true, want false")
	}

	// Non-tool mentions of the path (e.g. assistant prose) must not count.
	proseOnly := `{"method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"see skills/go-docs/SKILL.md"}}}}` + "\n"
	proseDir := filepath.Join(home, "sessions", "%2Ftmp%2Fws2", "sess-prose")
	if err := os.MkdirAll(proseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proseDir, "updates.jsonl"), []byte(proseOnly), 0o644); err != nil {
		t.Fatal(err)
	}
	if hit, _ := g.ScanLine([]byte(`{"type":"end","sessionId":"sess-prose"}`), "go-docs", ws); hit {
		t.Error("ScanLine(end, path only in prose) = true, want false")
	}
}

func TestGrokTriggerSpec(t *testing.T) {
	g := NewGrok()
	ws := t.TempDir()
	spec := g.TriggerSpec(ws, "use the skill", "grok-4.5", true)
	if !slices.Equal(spec.Argv[:3], []string{"grok", "-p", "use the skill"}) {
		t.Errorf("argv prefix = %v", spec.Argv[:3])
	}
	if !containsPair(spec.Argv, "--sandbox", "off") {
		t.Errorf("want --sandbox off when hostSandboxed: %v", spec.Argv)
	}
	if !containsPair(spec.Argv, "--output-format", "streaming-json") {
		t.Errorf("want streaming-json: %v", spec.Argv)
	}
	if !containsPair(spec.Argv, "--allow", "Skill") || !containsPair(spec.Argv, "--allow", "Read") {
		t.Errorf("want Skill+Read allows: %v", spec.Argv)
	}
	if !slices.Contains(spec.Argv, "--no-memory") {
		t.Errorf("want --no-memory: %v", spec.Argv)
	}
	assertNoYolo(t, spec.Argv)
	assertGrokIsolatedEnv(t, spec, ws)

	spec = g.TriggerSpec(ws, "q", "grok-4.5", false)
	if !containsPair(spec.Argv, "--sandbox", "workspace") {
		t.Errorf("want --sandbox workspace when unconfined: %v", spec.Argv)
	}
}

func TestGrokEvalSpec(t *testing.T) {
	g := NewGrok()
	ws := t.TempDir()
	spec := g.EvalSpec(ws, model.EvalInput{
		Prompt:        "fix it",
		HostSandboxed: true,
	}, "grok-4.5")
	if !containsPair(spec.Argv, "--sandbox", "off") {
		t.Errorf("want --sandbox off: %v", spec.Argv)
	}
	if !containsPair(spec.Argv, "--max-turns", "20") {
		t.Errorf("want default max-turns 20: %v", spec.Argv)
	}
	if !containsPair(spec.Argv, "--allow", "Read") || !containsPair(spec.Argv, "--allow", "Bash(terraform *)") {
		t.Errorf("want default allow rules: %v", spec.Argv)
	}
	if !slices.Contains(spec.Argv, "--no-memory") {
		t.Errorf("want --no-memory: %v", spec.Argv)
	}
	assertNoYolo(t, spec.Argv)
	assertGrokIsolatedEnv(t, spec, ws)

	spec = g.EvalSpec(ws, model.EvalInput{
		Prompt:        "x",
		MaxTurns:      5,
		AllowedTools:  "Read Grep",
		HostSandboxed: false,
	}, "grok-4.5")
	if !containsPair(spec.Argv, "--sandbox", "workspace") {
		t.Errorf("want workspace: %v", spec.Argv)
	}
	if !containsPair(spec.Argv, "--max-turns", "5") {
		t.Errorf("want max-turns 5: %v", spec.Argv)
	}
	// Case override replaces the default set entirely.
	if containsPair(spec.Argv, "--allow", "Write") {
		t.Errorf("did not expect Write allow with override: %v", spec.Argv)
	}
	if !containsPair(spec.Argv, "--allow", "Grep") {
		t.Errorf("want Grep allow: %v", spec.Argv)
	}
}

func TestGrokReportsUsage(t *testing.T) {
	if !NewGrok().ReportsUsage() {
		t.Error("ReportsUsage = false, want true")
	}
}

func assertGrokIsolatedEnv(t *testing.T, spec model.CommandSpec, ws string) {
	t.Helper()
	home := grokHomeDir(ws)
	want := []string{
		"GROK_HOME=" + home,
		"GROK_CLAUDE_SKILLS_ENABLED=false",
		"GROK_CURSOR_SKILLS_ENABLED=false",
		"GROK_DISABLE_AUTOUPDATER=1",
	}
	for _, e := range want {
		if !slices.Contains(spec.Env, e) {
			t.Errorf("Env missing %q; got %v", e, spec.Env)
		}
	}
	cfg := filepath.Join(home, "config.toml")
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("seeded config.toml: %v", err)
	}
	if !strings.Contains(string(data), "[compat.claude]") || !strings.Contains(string(data), "skills = false") {
		t.Errorf("config.toml missing isolation settings:\n%s", data)
	}
}

func TestGrokArmTriggerHit(t *testing.T) {
	g := NewGrok()
	ws := t.TempDir()
	sideHit, env := g.ArmTriggerHit(ws, "go-project")
	if sideHit == nil {
		t.Fatal("sideHit = nil")
	}
	if sideHit() {
		t.Error("sideHit true before marker written")
	}
	var hitFile, needle string
	for _, e := range env {
		switch {
		case strings.HasPrefix(e, "EVOLVE_HIT_FILE="):
			hitFile = strings.TrimPrefix(e, "EVOLVE_HIT_FILE=")
		case strings.HasPrefix(e, "EVOLVE_HIT_NEEDLE="):
			needle = strings.TrimPrefix(e, "EVOLVE_HIT_NEEDLE=")
		}
	}
	if hitFile == "" || needle != "skills/go-project/SKILL.md" {
		t.Fatalf("env = %v, want hit file + needle", env)
	}
	// Hook is seeded under the isolated home.
	home := grokHomeDir(ws)
	script := filepath.Join(home, "hooks", "evolve-trigger-hit.sh")
	if st, err := os.Stat(script); err != nil || st.Mode()&0o111 == 0 {
		t.Fatalf("hook script missing or not executable: %v", err)
	}
	// Simulate the hook creating the marker.
	if err := os.WriteFile(hitFile, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	if !sideHit() {
		t.Error("sideHit false after marker written")
	}
	// Concurrent arms get distinct hit files.
	_, env2 := g.ArmTriggerHit(ws, "go-project")
	if env2[0] == env[0] {
		t.Error("expected distinct EVOLVE_HIT_FILE for concurrent arms")
	}
}

// TestGrokTriggerHitHookScript marks only when the needle is present.
func TestGrokTriggerHitHookScript(t *testing.T) {
	ws := t.TempDir()
	g := NewGrok()
	sideHit, env := g.ArmTriggerHit(ws, "go-project")
	var hitFile, needle string
	for _, e := range env {
		switch {
		case strings.HasPrefix(e, "EVOLVE_HIT_FILE="):
			hitFile = strings.TrimPrefix(e, "EVOLVE_HIT_FILE=")
		case strings.HasPrefix(e, "EVOLVE_HIT_NEEDLE="):
			needle = strings.TrimPrefix(e, "EVOLVE_HIT_NEEDLE=")
		}
	}
	script := filepath.Join(grokHomeDir(ws), "hooks", "evolve-trigger-hit.sh")
	runHook := func(payload string) {
		t.Helper()
		cmd := exec.Command(script)
		cmd.Env = append(os.Environ(), "EVOLVE_HIT_FILE="+hitFile, "EVOLVE_HIT_NEEDLE="+needle)
		cmd.Stdin = strings.NewReader(payload)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("hook: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), `"decision":"allow"`) {
			t.Errorf("hook output = %q", out)
		}
	}
	runHook(`{"toolName":"read_file","toolInput":{"target_file":"/tmp/other.txt"}}`)
	if sideHit() {
		t.Error("hook marked hit on unrelated read")
	}
	_ = os.Remove(hitFile)
	runHook(`{"toolName":"read_file","toolInput":{"target_file":"/ws/.grok/skills/go-project/SKILL.md"}}`)
	if !sideHit() {
		t.Error("hook did not mark hit on skill read")
	}
}

// TestGrokLinksOperatorAuth ensures browser/session credentials from the
// operator GROK_HOME are available inside the isolated home (symlink preferred).
func TestGrokLinksOperatorAuth(t *testing.T) {
	opHome := t.TempDir()
	t.Setenv("GROK_HOME", opHome)
	authBody := []byte(`{"token":"session-from-browser"}`)
	if err := os.WriteFile(filepath.Join(opHome, "auth.json"), authBody, 0o600); err != nil {
		t.Fatal(err)
	}

	ws := t.TempDir()
	spec := NewGrok().TriggerSpec(ws, "q", "grok-4.5", true)
	isoHome := grokHomeDir(ws)
	if !slices.Contains(spec.Env, "GROK_HOME="+isoHome) {
		t.Fatalf("want isolated GROK_HOME in env, got %v", spec.Env)
	}
	// Child GROK_HOME must not be the operator home (isolation still holds).
	if isoHome == opHome {
		t.Fatal("isolated home must differ from operator GROK_HOME")
	}

	dst := filepath.Join(isoHome, "auth.json")
	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("auth.json in isolated home: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(authBody) {
		t.Errorf("auth.json body = %q, want %q", got, authBody)
	}
	// Prefer symlink so token refresh updates the operator credentials.
	if info.Mode()&os.ModeSymlink == 0 {
		t.Log("auth.json is a copy, not a symlink (acceptable fallback)")
	} else {
		target, err := os.Readlink(dst)
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(opHome, "auth.json")
		if target != want {
			t.Errorf("symlink target = %q, want %q", target, want)
		}
	}

	// No operator auth → isolated home has no auth.json (API-key-only CI).
	t.Setenv("GROK_HOME", t.TempDir())
	ws2 := t.TempDir()
	_ = NewGrok().TriggerSpec(ws2, "q", "grok-4.5", false)
	if _, err := os.Lstat(filepath.Join(grokHomeDir(ws2), "auth.json")); !os.IsNotExist(err) {
		t.Errorf("expected no auth.json without operator credentials, err=%v", err)
	}
}

func containsPair(argv []string, flag, value string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == value {
			return true
		}
	}
	return false
}

func assertNoYolo(t *testing.T, argv []string) {
	t.Helper()
	for _, a := range argv {
		if strings.Contains(strings.ToLower(a), "yolo") ||
			a == "--always-approve" ||
			a == "bypassPermissions" {
			t.Errorf("unexpected full-auto flag %q in %v", a, argv)
		}
	}
}

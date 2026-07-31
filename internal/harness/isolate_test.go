// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package harness

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/model"
)

func TestLinkFilePrefersSymlink(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.json")
	dst := filepath.Join(dir, "dst.json")
	body := []byte(`{"token":"x"}`)
	if err := os.WriteFile(src, body, 0o600); err != nil {
		t.Fatal(err)
	}

	linkFile(src, dst)
	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("dst after linkFile: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Log("dst is a copy, not a symlink (acceptable fallback)")
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Errorf("dst body = %q, want %q", got, body)
	}
}

func TestLinkFileNoOps(t *testing.T) {
	dir := t.TempDir()

	// Missing src leaves no dst behind.
	linkFile(filepath.Join(dir, "absent"), filepath.Join(dir, "dst"))
	if _, err := os.Lstat(filepath.Join(dir, "dst")); !os.IsNotExist(err) {
		t.Errorf("expected no dst for missing src, err=%v", err)
	}

	// An existing dst is never overwritten.
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "existing")
	os.WriteFile(src, []byte("new"), 0o600)
	os.WriteFile(dst, []byte("old"), 0o600)
	linkFile(src, dst)
	if got, _ := os.ReadFile(dst); string(got) != "old" {
		t.Errorf("existing dst overwritten: %q", got)
	}
}

func TestCopyFile0600(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.json")
	dst := filepath.Join(dir, "dst.json")
	os.WriteFile(src, []byte("secret"), 0o644)

	copyFile0600(src, dst)
	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("dst after copyFile0600: %v", err)
	}
	// Must be a private regular copy: run writes may not reach the source.
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("dst is a symlink, want a copy")
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("dst perm = %o, want 0600", perm)
	}

	// An existing dst is never overwritten.
	os.WriteFile(src, []byte("changed"), 0o644)
	copyFile0600(src, dst)
	if got, _ := os.ReadFile(dst); string(got) != "secret" {
		t.Errorf("existing dst overwritten: %q", got)
	}
}

// requireEnv fails unless env carries the exact entry.
func requireEnv(t *testing.T, env []string, entry string) {
	t.Helper()
	if !slices.Contains(env, entry) {
		t.Fatalf("want %q in env, got %v", entry, env)
	}
}

// TestClaudeKeychainService pins the per-config-dir Keychain service name to
// the value observed from claude 2.1.220 (security shim, see the function's
// doc comment). If the CLI changes its scheme this pin goes stale together
// with the bridge itself.
func TestClaudeKeychainService(t *testing.T) {
	got := claudeKeychainService("/Users/deavon/.config/claude")
	want := "Claude Code-credentials-c92fbf8b"
	if got != want {
		t.Errorf("claudeKeychainService = %q, want %q", got, want)
	}
}

func TestClaudeIsolation(t *testing.T) {
	opDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", opDir)
	// A set credential env var suppresses the machine-dependent Keychain
	// bridge, keeping this test hermetic on macOS dev machines.
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "tok-test")
	cred := []byte(`{"claudeAiOauth":{"accessToken":"x"}}`)
	if err := os.WriteFile(filepath.Join(opDir, ".credentials.json"), cred, 0o600); err != nil {
		t.Fatal(err)
	}

	ws := t.TempDir()
	iso := isolatedDir(ws, claudeConfigRel)
	spec := NewClaude().TriggerSpec(ws, "q", "m", false)
	requireEnv(t, spec.Env, "CLAUDE_CONFIG_DIR="+iso)
	eval := NewClaude().EvalSpec(ws, model.EvalInput{Prompt: "p"}, "m")
	requireEnv(t, eval.Env, "CLAUDE_CONFIG_DIR="+iso)
	if iso == opDir {
		t.Fatal("isolated config dir must differ from operator CLAUDE_CONFIG_DIR")
	}

	// Onboarding state seeded so headless -p never stalls on first run.
	state, err := os.ReadFile(filepath.Join(iso, ".claude.json"))
	if err != nil {
		t.Fatalf(".claude.json: %v", err)
	}
	if !strings.Contains(string(state), "hasCompletedOnboarding") {
		t.Errorf(".claude.json = %q", state)
	}

	// Operator OAuth credentials bridged (Linux keeps them beside the config).
	got, err := os.ReadFile(filepath.Join(iso, ".credentials.json"))
	if err != nil {
		t.Fatalf(".credentials.json in isolated dir: %v", err)
	}
	if string(got) != string(cred) {
		t.Errorf(".credentials.json body = %q, want %q", got, cred)
	}

	// No operator credentials → nothing bridged (macOS Keychain / env-key CI).
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	ws2 := t.TempDir()
	_ = NewClaude().TriggerSpec(ws2, "q", "m", false)
	if _, err := os.Lstat(filepath.Join(isolatedDir(ws2, claudeConfigRel), ".credentials.json")); !os.IsNotExist(err) {
		t.Errorf("expected no bridged credentials, err=%v", err)
	}
}

func TestCodexIsolation(t *testing.T) {
	opHome := t.TempDir()
	t.Setenv("CODEX_HOME", opHome)
	auth := []byte(`{"OPENAI_API_KEY":null,"tokens":{}}`)
	os.WriteFile(filepath.Join(opHome, "auth.json"), auth, 0o600)
	os.WriteFile(filepath.Join(opHome, "config.toml"), []byte(
		"model = \"gpt-5.2\"\ncli_auth_credentials_store = \"keyring\"\n[mcp_servers.github]\ncommand = \"gh-mcp\"\n"), 0o644)

	ws := t.TempDir()
	iso := isolatedDir(ws, codexHomeRel)
	spec := NewCodex().TriggerSpec(ws, "q", "m", false)
	requireEnv(t, spec.Env, "CODEX_HOME="+iso)
	eval := NewCodex().EvalSpec(ws, model.EvalInput{Prompt: "p"}, "m")
	requireEnv(t, eval.Env, "CODEX_HOME="+iso)

	got, err := os.ReadFile(filepath.Join(iso, "auth.json"))
	if err != nil {
		t.Fatalf("auth.json in isolated home: %v", err)
	}
	if string(got) != string(auth) {
		t.Errorf("auth.json body = %q, want %q", got, auth)
	}

	// Seeded config carries only the credential-store selection — never the
	// operator's MCP servers, profiles, or trust.
	cfg, err := os.ReadFile(filepath.Join(iso, "config.toml"))
	if err != nil {
		t.Fatalf("config.toml in isolated home: %v", err)
	}
	if !strings.Contains(string(cfg), `cli_auth_credentials_store = "keyring"`) {
		t.Errorf("config.toml missing credential store: %q", cfg)
	}
	if strings.Contains(string(cfg), "mcp_servers") || strings.Contains(string(cfg), "gpt-5.2") {
		t.Errorf("config.toml leaked operator config: %q", cfg)
	}

	// No credential-store selection → no config.toml seeded at all.
	op2 := t.TempDir()
	t.Setenv("CODEX_HOME", op2)
	os.WriteFile(filepath.Join(op2, "config.toml"), []byte("model = \"gpt-5.2\"\n"), 0o644)
	ws2 := t.TempDir()
	_ = NewCodex().TriggerSpec(ws2, "q", "m", false)
	if _, err := os.Lstat(filepath.Join(isolatedDir(ws2, codexHomeRel), "config.toml")); !os.IsNotExist(err) {
		t.Errorf("expected no seeded config.toml, err=%v", err)
	}
}

func TestGeminiIsolation(t *testing.T) {
	opHome := t.TempDir()
	t.Setenv("HOME", opHome)
	opGemini := filepath.Join(opHome, ".gemini")
	os.MkdirAll(opGemini, 0o755)
	creds := []byte(`{"access_token":"x"}`)
	os.WriteFile(filepath.Join(opGemini, "oauth_creds.json"), creds, 0o600)
	os.WriteFile(filepath.Join(opGemini, "settings.json"), []byte(`{"selectedAuthType":"oauth-personal"}`), 0o644)
	os.WriteFile(filepath.Join(opHome, ".gitconfig"), []byte("[user]\n\tname = op\n"), 0o644)

	ws := t.TempDir()
	fake := isolatedDir(ws, geminiHomeRel)
	spec := NewGemini().TriggerSpec(ws, "q", "m", true)
	requireEnv(t, spec.Env, "HOME="+fake)
	// Isolation env must not displace the nested-sandbox toggle.
	requireEnv(t, spec.Env, "GEMINI_SANDBOX=false")

	got, err := os.ReadFile(filepath.Join(fake, ".gemini", "oauth_creds.json"))
	if err != nil {
		t.Fatalf("oauth_creds.json in fake HOME: %v", err)
	}
	if string(got) != string(creds) {
		t.Errorf("oauth_creds.json body = %q, want %q", got, creds)
	}
	// settings.json is mutable → private copy, never a symlink.
	info, err := os.Lstat(filepath.Join(fake, ".gemini", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json in fake HOME: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("settings.json is a symlink, want a copy")
	}
	// Git identity bridged into the fake HOME.
	if _, err := os.Lstat(filepath.Join(fake, ".gitconfig")); err != nil {
		t.Errorf(".gitconfig in fake HOME: %v", err)
	}
}

func TestCursorIsolation(t *testing.T) {
	opDir := t.TempDir()
	t.Setenv("CURSOR_CONFIG_DIR", opDir)
	cfg := []byte(`{"accessToken":"x","version":1}`)
	os.WriteFile(filepath.Join(opDir, "cli-config.json"), cfg, 0o600)

	ws := t.TempDir()
	fake := isolatedDir(ws, cursorHomeRel)
	iso := filepath.Join(fake, ".cursor")
	spec := NewCursor().TriggerSpec(ws, "q", "m", false)
	requireEnv(t, spec.Env, "HOME="+fake)
	requireEnv(t, spec.Env, "CURSOR_CONFIG_DIR="+iso)
	eval := NewCursor().EvalSpec(ws, model.EvalInput{Prompt: "p"}, "m")
	requireEnv(t, eval.Env, "CURSOR_CONFIG_DIR="+iso)

	// cli-config.json mixes auth with mutable config → private copy only.
	info, err := os.Lstat(filepath.Join(iso, "cli-config.json"))
	if err != nil {
		t.Fatalf("cli-config.json in isolated dir: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("cli-config.json is a symlink, want a copy")
	}
	got, _ := os.ReadFile(filepath.Join(iso, "cli-config.json"))
	if string(got) != string(cfg) {
		t.Errorf("cli-config.json body = %q, want %q", got, cfg)
	}
}

func TestCopilotIsolation(t *testing.T) {
	opHome := t.TempDir()
	t.Setenv("COPILOT_HOME", opHome)
	cfg := []byte(`{"trusted_folders":[]}`)
	os.WriteFile(filepath.Join(opHome, "config.json"), cfg, 0o600)

	ws := t.TempDir()
	iso := isolatedDir(ws, copilotHomeRel)
	spec := NewCopilot().TriggerSpec(ws, "q", "m", false)
	requireEnv(t, spec.Env, "COPILOT_HOME="+iso)
	eval := NewCopilot().EvalSpec(ws, model.EvalInput{Prompt: "p"}, "m")
	requireEnv(t, eval.Env, "COPILOT_HOME="+iso)

	info, err := os.Lstat(filepath.Join(iso, "config.json"))
	if err != nil {
		t.Fatalf("config.json in isolated home: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("config.json is a symlink, want a copy")
	}
}

func TestAntigravityIsolation(t *testing.T) {
	opHome := t.TempDir()
	t.Setenv("HOME", opHome)
	opDir := filepath.Join(opHome, ".gemini", "antigravity-cli")
	os.MkdirAll(opDir, 0o755)
	token := []byte("oauth-token")
	os.WriteFile(filepath.Join(opDir, "antigravity-oauth-token"), token, 0o600)

	ws := t.TempDir()
	fake := isolatedDir(ws, agyHomeRel)
	spec := NewAntigravity().TriggerSpec(ws, "q", "m", false)
	requireEnv(t, spec.Env, "HOME="+fake)
	eval := NewAntigravity().EvalSpec(ws, model.EvalInput{Prompt: "p"}, "m")
	requireEnv(t, eval.Env, "HOME="+fake)

	got, err := os.ReadFile(filepath.Join(fake, ".gemini", "antigravity-cli", "antigravity-oauth-token"))
	if err != nil {
		t.Fatalf("oauth token in fake HOME: %v", err)
	}
	if string(got) != string(token) {
		t.Errorf("oauth token body = %q, want %q", got, token)
	}
}

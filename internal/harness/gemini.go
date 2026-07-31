// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/model"
)

// Gemini drives the `gemini` CLI. It has no behavioral-eval runner yet, so it
// implements only the required Harness surface (no EvalRunner); engines
// type-assert and degrade, so its models are token-counted but not run for evals.
type Gemini struct {
	base
}

// NewGemini returns the builtin Gemini harness.
func NewGemini() *Gemini {
	return &Gemini{base: base{
		id:        model.HarnessGemini,
		name:      "Gemini CLI",
		clis:      []string{"gemini"},
		envKeys:   []string{"EVOLVE_GOOGLE_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY"},
		skillDirs: []string{filepath.Join(".gemini", "skills")},
	}}
}

// geminiHomeRel is the workspace-relative fake $HOME evolve gives the gemini
// CLI. The CLI hard-wires its state root to <home>/.gemini with no dedicated
// config-dir variable, so isolation overrides HOME itself: session chats,
// history, and save_memory's GEMINI.md all land here and die with the
// workspace. Project skills stay at .gemini/skills (the skillDirs mount).
const geminiHomeRel = ".evolve/gemini-home"

// geminiEnv returns the process env extras for a gemini invocation in ws: a
// fake HOME with the operator's OAuth credentials bridged in.
func geminiEnv(ws string) []string {
	home := isolatedDir(ws, geminiHomeRel)
	ensureGeminiHome(home)
	return []string{"HOME=" + home}
}

// ensureGeminiHome creates <fake>/.gemini, links the operator's OAuth
// credentials (write-through so a mid-run token refresh sticks), copies
// settings.json (it carries the selected auth type but is mutable, so a copy
// keeps run writes out of the real file), and bridges git config into the fake
// HOME. Best-effort per the isolate.go contract.
func ensureGeminiHome(home string) {
	dir := filepath.Join(home, ".gemini")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	src := operatorDir("", ".gemini")
	if src == "" || sameFilePath(src, dir) {
		return
	}
	for _, name := range []string{"oauth_creds.json", "google_accounts.json", "installation_id"} {
		linkFile(filepath.Join(src, name), filepath.Join(dir, name))
	}
	copyFile0600(filepath.Join(src, "settings.json"), filepath.Join(dir, "settings.json"))
	linkGitConfig(home)
}

// TriggerSpec builds the gemini invocation. Only --output-format stream-json
// emits tool_use events; --skip-trust keeps headless runs alive when the
// folder-trust feature is enabled (temp workspaces are never trusted).
func (g *Gemini) TriggerSpec(ws, query, cliModelID string, hostSandboxed bool) model.CommandSpec {
	spec := model.CommandSpec{
		Argv: []string{"gemini", "-p", query, "-m", cliModelID, "--output-format", "stream-json", "--skip-trust"},
		Dir:  ws,
		Env:  geminiEnv(ws),
	}
	if hostSandboxed {
		// gemini's own sandbox (GEMINI_SANDBOX=docker|podman|sandbox-exec) cannot
		// nest inside evolve's; force it off so evolve's sandbox is the only layer
		// even when the surrounding environment enabled it.
		spec.Env = append(spec.Env, "GEMINI_SANDBOX=false")
	}
	return spec
}

// ScanLine reports a hit when an activate_skill tool_use names the skill (a read
// of the SKILL.md path counts as a fallback). A result/error event becomes a
// warning note and counts as no-trigger.
func (g *Gemini) ScanLine(line []byte, skill, _ string) (bool, string) {
	var event struct {
		Type       string          `json:"type"`
		Status     string          `json:"status"`
		ToolName   string          `json:"tool_name"`
		Parameters json.RawMessage `json:"parameters"`
		Error      struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(line, &event) != nil {
		return false, ""
	}
	if event.Type == "result" && event.Status == "error" {
		message := event.Error.Message
		if len(message) > 200 {
			message = message[:200]
		}
		return false, "gemini run errored; counted as no-trigger: " + message
	}
	if event.Type != "tool_use" {
		return false, ""
	}
	payload := string(event.Parameters)
	if event.ToolName == "activate_skill" && strings.Contains(payload, skill) {
		return true, ""
	}
	return strings.Contains(payload, "skills/"+skill+"/SKILL.md"), ""
}

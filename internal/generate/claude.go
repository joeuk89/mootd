package generate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type claude struct {
	bin       string
	configDir string
	model     string
	effort    string
}

// FindClaude locates the claude command: the configured path, then PATH, then the usual install places.
func FindClaude(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	if path, err := exec.LookPath("claude"); err == nil {
		return path, nil
	}
	// The shell startup file may run mootd before it has finished building PATH.
	home, _ := os.UserHomeDir()
	for _, path := range []string{
		filepath.Join(home, ".local", "bin", "claude"),
		filepath.Join(home, ".claude", "local", "claude"),
		"/opt/homebrew/bin/claude",
		"/usr/local/bin/claude",
	} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", errors.New("the claude command was not found; set claude_bin in the config")
}

// call runs one headless prompt and decodes the structured reply into out.
//
// The flags switch off tools, MCP servers, hooks, skills and settings files. The
// prompt holds headlines from the internet, so the model must be able to do nothing
// except return text, and the user's own Claude setup must not leak into the jokes.
func (c claude) call(ctx context.Context, system, prompt string, schema object, out any) error {
	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		return err
	}
	args := []string{
		"-p",
		"--model", c.model,
		"--system-prompt", system,
		"--json-schema", string(schemaJSON),
		"--output-format", "json",
		"--tools", "",
		"--strict-mcp-config",
		"--setting-sources", "",
		"--settings", `{"disableAllHooks":true}`,
		"--disable-slash-commands",
		"--no-session-persistence",
	}
	if c.effort != "" {
		args = append(args, "--effort", c.effort)
	}

	// An empty working directory keeps any project CLAUDE.md out of the call.
	dir, err := os.MkdirTemp("", "mootd-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	cmd := exec.CommandContext(ctx, c.bin, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(prompt)
	// MOOTD_SKIP stops a shell started by claude from printing or triggering mootd again.
	cmd.Env = append(os.Environ(), "MOOTD_SKIP=1")
	if c.configDir != "" {
		cmd.Env = append(cmd.Env, "CLAUDE_CONFIG_DIR="+c.configDir)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("claude timed out: %w", ctx.Err())
		}
		return fmt.Errorf("claude failed: %w: %s", err, firstLine(stderr.String(), stdout.String()))
	}

	var envelope struct {
		IsError          bool            `json:"is_error"`
		Result           string          `json:"result"`
		StructuredOutput json.RawMessage `json:"structured_output"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		return fmt.Errorf("claude returned unreadable output: %w", err)
	}
	if envelope.IsError {
		return fmt.Errorf("claude reported an error: %s", firstLine(envelope.Result))
	}
	payload := []byte(envelope.StructuredOutput)
	if len(payload) == 0 || string(payload) == "null" {
		payload = []byte(envelope.Result)
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("claude returned an unexpected reply: %w", err)
	}
	return nil
}

func firstLine(candidates ...string) string {
	for _, s := range candidates {
		if s = strings.TrimSpace(s); s != "" {
			line, _, _ := strings.Cut(s, "\n")
			if len(line) > 300 {
				line = line[:300]
			}
			return line
		}
	}
	return "no output"
}

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/joeuk89/mootd/internal/config"
	"github.com/joeuk89/mootd/internal/generate"
	"github.com/joeuk89/mootd/internal/shellhook"
	"github.com/joeuk89/mootd/internal/store"
)

type account struct {
	LoggedIn     bool   `json:"loggedIn"`
	Email        string `json:"email"`
	Subscription string `json:"subscriptionType"`
}

func (a account) String() string {
	switch {
	case !a.LoggedIn:
		return "not logged in"
	case a.Subscription != "":
		return fmt.Sprintf("%s (%s)", a.Email, a.Subscription)
	default:
		return a.Email
	}
}

func initCommand(args []string) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	configDir := flags.String("claude-config-dir", "", "the Claude config directory to use")
	yes := flags.Bool("yes", false, "answer yes to every question")
	if err := flags.Parse(args); err != nil {
		return err
	}
	input := bufio.NewReader(os.Stdin)

	existing, err := config.Load()
	if err != nil {
		return err
	}
	claude, err := generate.FindClaude(existing.ClaudeBin)
	if err != nil {
		return errors.New("Claude Code is not installed. Install it from https://claude.com/claude-code, sign in, then run \"mootd init\" again")
	}
	fmt.Println("Claude Code:", tilde(claude))

	dir := firstNonEmpty(*configDir, existing.ClaudeConfigDir, os.Getenv("CLAUDE_CONFIG_DIR"))
	if dir == "" {
		if dir, err = chooseAccount(claude, input, *yes); err != nil {
			return err
		}
	}
	who, err := authStatus(claude, dir)
	switch {
	case err != nil:
		fmt.Println("Account:     could not check the login:", err)
	case !who.LoggedIn:
		return errors.New("Claude Code is not logged in. Run \"claude\", sign in, then run \"mootd init\" again")
	default:
		fmt.Println("Account:    ", who)
	}

	path, err := config.Path()
	if err != nil {
		return err
	}
	switch _, statErr := os.Stat(path); {
	case errors.Is(statErr, fs.ErrNotExist):
		if err := writeConfig(path, dir); err != nil {
			return err
		}
		fmt.Println("Config:      created", tilde(path))
	case dir != existing.ClaudeConfigDir:
		fmt.Printf("Config:      %s already exists. To use this account, set:\n               claude_config_dir = %q\n", tilde(path), tilde(dir))
	default:
		fmt.Println("Config:      keeping", tilde(path))
	}

	if err := installHook(input, *yes); err != nil {
		return err
	}
	fmt.Println("\nHere is your first greeting. Today's batch is being written in the background;")
	fmt.Println("it takes a few minutes, and new terminals show built-in greetings until then.")
	return show()
}

// chooseAccount returns the Claude config directory to use, or "" for Claude's default.
// It only asks when there is a real choice to make.
func chooseAccount(claude string, input *bufio.Reader, yes bool) (string, error) {
	dirs := accountDirs()
	if len(dirs) < 2 {
		return "", nil
	}
	if yes {
		return "", errors.New("you have more than one Claude account; pass --claude-config-dir to pick one")
	}
	fmt.Println("\nYou have more than one Claude account. Which one should write your greetings?")
	for i, dir := range dirs {
		who, err := authStatus(claude, dir)
		label := who.String()
		if err != nil {
			label = "unknown"
		}
		fmt.Printf("  %d) %-24s %s\n", i+1, tilde(dir), label)
	}
	for {
		fmt.Printf("Choose 1-%d: ", len(dirs))
		line, err := input.ReadString('\n')
		if n, convErr := strconv.Atoi(strings.TrimSpace(line)); convErr == nil && n >= 1 && n <= len(dirs) {
			fmt.Println()
			return dirs[n-1], nil
		}
		if err != nil {
			return "", errors.New("no account chosen")
		}
	}
}

// accountDirs lists Claude config directories: the default ~/.claude, plus any
// ~/.claude-* that holds an account file. Symlinks to the same place count once.
func accountDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	candidates := []string{filepath.Join(home, ".claude")}
	others, _ := filepath.Glob(filepath.Join(home, ".claude-*"))
	for _, dir := range others {
		if _, err := os.Stat(filepath.Join(dir, ".claude.json")); err == nil {
			candidates = append(candidates, dir)
		}
	}

	var dirs []string
	seen := map[string]bool{}
	for _, dir := range candidates {
		real, err := filepath.EvalSymlinks(dir)
		if err != nil || seen[real] {
			continue
		}
		if info, err := os.Stat(real); err != nil || !info.IsDir() {
			continue
		}
		seen[real] = true
		dirs = append(dirs, real)
	}
	return dirs
}

func authStatus(claude, configDir string) (account, error) {
	cmd := exec.Command(claude, "auth", "status")
	cmd.Env = append(os.Environ(), "MOOTD_SKIP=1")
	if configDir != "" {
		cmd.Env = append(cmd.Env, "CLAUDE_CONFIG_DIR="+configDir)
	}
	// A logged-out claude exits non-zero but still prints its status, so read the output first.
	out, runErr := cmd.Output()
	var who account
	if err := json.Unmarshal(out, &who); err != nil {
		if runErr != nil {
			return who, runErr
		}
		return who, errors.New("this version of Claude Code does not report its login status")
	}
	return who, nil
}

func writeConfig(path, claudeConfigDir string) error {
	if err := config.Create(path); err != nil {
		return err
	}
	if claudeConfigDir == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	const placeholder = `#claude_config_dir = ""`
	setting := fmt.Sprintf("claude_config_dir = %q", tilde(claudeConfigDir))
	return os.WriteFile(path, []byte(strings.Replace(string(data), placeholder, setting, 1)), 0o644)
}

func installHook(input *bufio.Reader, yes bool) error {
	rc, err := startupFile()
	if err != nil {
		fmt.Println("Shell:      ", err)
		return nil
	}
	content, mode, err := readStartupFile(rc)
	if err != nil {
		return err
	}
	if shellhook.Installed(content) {
		fmt.Println("Shell:       already hooked into", tilde(rc))
		return nil
	}

	block := shellhook.Block(hookBinary())
	updated, aboveInstantPrompt := shellhook.Insert(content, block)
	where := "at the end of " + tilde(rc)
	if aboveInstantPrompt {
		where = "to " + tilde(rc) + ", above the Powerlevel10k instant prompt"
	}
	fmt.Printf("\nTo show a greeting in each new terminal, mootd adds these lines %s:\n\n", where)
	for _, line := range strings.Split(strings.TrimSuffix(block, "\n"), "\n") {
		fmt.Println("    " + line)
	}
	if !yes {
		fmt.Print("\nAdd them? [y/N] ")
		answer, _ := input.ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			fmt.Println("Skipped. Run \"mootd init\" again when you want the hook.")
			return nil
		}
	}
	if err := writeStartupFile(rc, updated, mode); err != nil {
		return err
	}
	fmt.Println("Shell:       hooked into", tilde(rc))
	return nil
}

func uninstall(args []string) error {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	purge := flags.Bool("purge", false, "also delete the settings and every greeting")
	if err := flags.Parse(args); err != nil {
		return err
	}

	for _, rc := range startupFiles() {
		content, mode, err := readStartupFile(rc)
		if err != nil {
			return err
		}
		updated, removed := shellhook.Remove(content)
		if !removed {
			continue
		}
		if err := writeStartupFile(rc, updated, mode); err != nil {
			return err
		}
		fmt.Println("Removed the hook from", tilde(rc))
	}

	if *purge {
		path, err := config.Path()
		if err != nil {
			return err
		}
		st, err := store.Open()
		if err != nil {
			return err
		}
		for _, dir := range []string{filepath.Dir(path), st.Dir()} {
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
			fmt.Println("Deleted", tilde(dir))
		}
	}
	fmt.Println("To remove the program itself, run: brew uninstall mootd")
	return nil
}

func startupFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch shell := filepath.Base(os.Getenv("SHELL")); shell {
	case "zsh":
		return filepath.Join(firstNonEmpty(os.Getenv("ZDOTDIR"), home), ".zshrc"), nil
	case "bash":
		// macOS terminals start bash as a login shell, which reads .bash_profile, not .bashrc.
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, ".bash_profile"), nil
		}
		return filepath.Join(home, ".bashrc"), nil
	default:
		return "", fmt.Errorf("mootd hooks into zsh and bash, but your shell is %q. Run \"mootd\" from its startup file yourself", shell)
	}
}

func startupFiles() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(firstNonEmpty(os.Getenv("ZDOTDIR"), home), ".zshrc"),
		filepath.Join(home, ".bash_profile"),
		filepath.Join(home, ".bashrc"),
	}
}

func readStartupFile(path string) (string, fs.FileMode, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", 0o644, nil
	}
	if err != nil {
		return "", 0, err
	}
	data, err := os.ReadFile(path)
	return string(data), info.Mode().Perm(), err
}

// writeStartupFile replaces the file in one step. It writes through a symlink rather
// than over it, since startup files often link into a dotfiles repository.
func writeStartupFile(path, content string, mode fs.FileMode) error {
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
	tmp := path + ".mootd-tmp"
	if err := os.WriteFile(tmp, []byte(content), mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// hookBinary is the path the shell hook runs. It prefers the PATH entry, such as
// /opt/homebrew/bin/mootd, over the versioned file it links to, so the hook
// survives upgrades.
func hookBinary() string {
	self, err := os.Executable()
	if err != nil {
		return "mootd"
	}
	onPath, err := exec.LookPath("mootd")
	if err != nil {
		return self
	}
	a, errA := os.Stat(onPath)
	b, errB := os.Stat(self)
	if errA == nil && errB == nil && os.SameFile(a, b) {
		return onPath
	}
	return self
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

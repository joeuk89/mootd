package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/joeuk89/mootd/internal/config"
	"github.com/joeuk89/mootd/internal/store"
)

func status() error {
	now := time.Now()
	st, err := store.Open()
	if err != nil {
		return err
	}
	var pool store.Pool
	if err := st.Update(func(p *store.Pool) error { pool = *p; return nil }); err != nil {
		return err
	}
	kept, noped, err := st.RecentFeedback(1 << 30)
	if err != nil {
		return err
	}

	gen := pool.Generation
	fmt.Println("Generation")
	switch {
	case st.GenerationRunning():
		row("Status", "running now")
	case gen.FailingFor(now) > 0:
		row("Status", "FAILING since "+when(gen.FailingSince, now))
		row("Error", gen.LastError)
	case gen.LastAttempt.IsZero():
		row("Status", "not run yet; the next terminal you open starts it")
	default:
		row("Status", "ok")
	}
	row("Last success", when(gen.LastSuccess, now))
	row("Last attempt", when(gen.LastAttempt, now))
	if log := st.LatestLog(); log != "" {
		row("Log", tilde(log))
	}

	topical, keptInPool, unseen := 0, 0, 0
	for _, e := range pool.Entries {
		if e.Expires != "" {
			topical++
		}
		if e.Kept {
			keptInPool++
		}
		if e.LastShown.IsZero() {
			unseen++
		}
	}
	fmt.Println("\nPool")
	row("Greetings", fmt.Sprintf("%d (%d topical, %d permanent)", len(pool.Entries), topical, len(pool.Entries)-topical))
	row("Not yet shown", fmt.Sprint(unseen))
	row("Your feedback", fmt.Sprintf("%d kept, %d dropped", len(kept), len(noped)))
	row("Data", tilde(st.Dir()))

	fmt.Println("\nSettings")
	path, err := config.Path()
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	switch _, statErr := os.Stat(path); {
	case err != nil:
		row("Config", "ERROR: "+err.Error())
		return nil
	case errors.Is(statErr, fs.ErrNotExist):
		row("Config", tilde(path)+" (not created; using the defaults)")
	default:
		row("Config", tilde(path))
	}
	row("Model", strings.TrimSpace(cfg.Model+" "+cfg.Effort))
	if cfg.ClaudeConfigDir != "" {
		row("Claude config", tilde(cfg.ClaudeConfigDir))
	}
	return nil
}

func row(label, value string) {
	fmt.Printf("  %-14s %s\n", label+":", value)
}

func when(t, now time.Time) string {
	if t.IsZero() {
		return "never"
	}
	t = t.Local()
	day := func(t time.Time) string { return t.Format("2006-01-02") }
	switch day(t) {
	case day(now):
		return "today at " + t.Format("15:04")
	case day(now.AddDate(0, 0, -1)):
		return "yesterday at " + t.Format("15:04")
	default:
		return t.Format("2 Jan at 15:04")
	}
}

func tilde(path string) string {
	if home, err := os.UserHomeDir(); err == nil {
		if rest, ok := strings.CutPrefix(path, home+"/"); ok {
			return "~/" + rest
		}
	}
	return path
}

// editConfig opens the config file in the user's editor, creating it from the
// template first, then checks that the result still loads.
func editConfig() error {
	path, err := config.Path()
	if err != nil {
		return err
	}
	if err := config.Create(path); err != nil {
		return err
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	// The editor setting may carry its own flags, such as "code --wait", so a shell splits it.
	cmd := exec.Command("sh", "-c", editor+` "$1"`, "sh", path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("the editor failed: %w", err)
	}
	if _, err := config.Load(); err != nil {
		return fmt.Errorf("the config has an error and will be ignored until it is fixed: %w", err)
	}
	return nil
}

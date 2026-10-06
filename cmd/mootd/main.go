package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/joeuk89/mootd/internal/config"
	"github.com/joeuk89/mootd/internal/generate"
	"github.com/joeuk89/mootd/internal/greeting"
	"github.com/joeuk89/mootd/internal/render"
	"github.com/joeuk89/mootd/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		// A greeting must never break or clutter shell startup, so errors stay quiet.
		if err := show(); err != nil && os.Getenv("MOOTD_DEBUG") != "" {
			fmt.Fprintln(os.Stderr, "mootd:", err)
		}
		return
	}

	var err error
	switch os.Args[1] {
	case "generate":
		err = runGenerate()
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "mootd:", err)
		os.Exit(1)
	}
}

func show() error {
	if os.Getenv("MOOTD_SKIP") != "" {
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if inSkippedTerminal(cfg.SkipTerminals) {
		return nil
	}
	opts, rows := terminalOptions()
	opts.ShowSource = cfg.ShowSource
	st, err := store.Open()
	if err != nil {
		return err
	}

	var picked *greeting.Greeting
	generationDue := false
	err = st.Update(func(p *store.Pool) error {
		now := time.Now()
		// Claiming the attempt under the pool lock means that when several terminals
		// open at once, only one of them starts a generation.
		if p.Generation.Due(now) {
			p.Generation.LastAttempt = now
			generationDue = true
		}
		p.Prune(now)
		fits := func(g greeting.Greeting) bool { return render.Fits(g, opts, rows) }
		entry := p.Pick(now, fits, rand.Shuffle)
		if entry == nil {
			return nil
		}
		entry.LastShown = now
		if window := store.WindowKey(); window != "" {
			p.SetWindow(window, entry.Greeting.ID)
		}
		picked = &entry.Greeting
		return nil
	})
	if err != nil {
		return err
	}
	if picked != nil {
		fmt.Print(render.Render(*picked, opts))
	}
	if generationDue {
		return startBackgroundGeneration()
	}
	return nil
}

func inSkippedTerminal(names []string) bool {
	current := []string{os.Getenv("TERM_PROGRAM"), os.Getenv("TERMINAL_EMULATOR")}
	for _, name := range names {
		for _, c := range current {
			if c != "" && strings.EqualFold(name, c) {
				return true
			}
		}
	}
	return false
}

func terminalOptions() (render.Options, int) {
	opts := render.Options{Width: 80}
	rows := math.MaxInt
	fd := int(os.Stdout.Fd())
	if !term.IsTerminal(fd) {
		return opts, rows
	}
	// Some pseudo-terminals report a size of 0x0; keep the defaults for those.
	if w, h, err := term.GetSize(fd); err == nil && w > 0 && h > 0 {
		opts.Width, rows = w, h
	}
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return opts, rows
	}
	opts.Hyperlinks = true
	opts.Colour = render.Colour256
	if ct := os.Getenv("COLORTERM"); ct == "truecolor" || ct == "24bit" {
		opts.Colour = render.TrueColour
	}
	return opts, rows
}

// startBackgroundGeneration runs "mootd generate" in its own session, so it outlives
// this process and the terminal window that started it.
func startBackgroundGeneration() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "generate")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func runGenerate() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st, err := store.Open()
	if err != nil {
		return err
	}
	release, ok, err := st.LockGeneration()
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("a generation is already running")
	}
	defer release()

	now := time.Now()
	logFile, err := st.OpenLog(now)
	if err != nil {
		return err
	}
	defer logFile.Close()
	var out io.Writer = logFile
	if term.IsTerminal(int(os.Stderr.Fd())) {
		out = io.MultiWriter(logFile, os.Stderr)
	}
	logger := log.New(out, "", log.LstdFlags)

	if err := st.Update(func(p *store.Pool) error {
		p.Generation.LastAttempt = now
		return nil
	}); err != nil {
		return err
	}

	gen := generate.Generator{Config: cfg, Store: st, Log: logger, HTTP: http.DefaultClient}
	_, runErr := gen.Run(context.Background(), now)
	if runErr != nil {
		logger.Printf("generation failed: %v", runErr)
	}
	if err := st.Update(func(p *store.Pool) error {
		p.Generation.Record(time.Now(), runErr)
		return nil
	}); err != nil {
		return err
	}
	return runErr
}

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

	"github.com/joeuk89/mootd/internal/builtin"
	"github.com/joeuk89/mootd/internal/config"
	"github.com/joeuk89/mootd/internal/generate"
	"github.com/joeuk89/mootd/internal/greeting"
	"github.com/joeuk89/mootd/internal/render"
	"github.com/joeuk89/mootd/internal/store"
)

const usage = `mootd prints an AI-written greeting each time you open a terminal.

  mootd                 Print the next greeting
  mootd keep            Keep this window's greeting for good, and ask for more like it
  mootd nope [reason]   Drop this window's greeting, and ask for fewer like it
  mootd open            Open this window's news story in the browser
  mootd generate        Make a new batch now
  mootd status          Show the pool, the last generation and any errors
  mootd config          Edit the settings
  mootd init            Set mootd up: check Claude Code, write the config, hook into your shell
  mootd uninstall       Remove the shell hook; add --purge to delete settings and greetings too
  mootd version         Print the version
`

// Set at build time by the release script.
var version = "dev"

// How long generation must have been failing before the greeting mentions it.
const failureWarningAfter = 72 * time.Hour

func main() {
	if len(os.Args) < 2 {
		// A greeting must never break or clutter shell startup, so errors stay quiet.
		if err := show(); err != nil && os.Getenv("MOOTD_DEBUG") != "" {
			fmt.Fprintln(os.Stderr, "mootd:", err)
		}
		return
	}

	var err error
	switch args := os.Args[2:]; os.Args[1] {
	case "keep":
		err = keep()
	case "nope":
		err = nope(strings.Join(args, " "))
	case "open":
		err = openSource()
	case "generate":
		err = runGenerate()
	case "status":
		err = status()
	case "config":
		err = editConfig()
	case "init":
		err = initCommand(args)
	case "uninstall":
		err = uninstall(args)
	case "version", "--version":
		fmt.Println("mootd", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		err = fmt.Errorf("unknown command %q; run \"mootd help\"", os.Args[1])
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
		hint("the config file has an error")
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
	generationDue, failing := false, false
	err = st.Update(func(p *store.Pool) error {
		now := time.Now()
		if p.Builtins < builtin.Version {
			greetings, err := builtin.Greetings()
			if err != nil {
				return err
			}
			if err := st.SeedBuiltins(p, builtin.Version, greetings); err != nil {
				return err
			}
		}
		// Claiming the attempt under the pool lock means that when several terminals
		// open at once, only one of them starts a generation.
		if p.Generation.Due(now) {
			p.Generation.LastAttempt = now
			generationDue = true
		}
		failing = p.Generation.FailingFor(now) >= failureWarningAfter
		p.Prune(now, cfg.EvergreenLimit)
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
	if failing {
		hint("generation is failing")
	}
	if generationDue {
		return startBackgroundGeneration()
	}
	return nil
}

// hint prints the one line mootd allows itself at shell startup when something needs fixing.
func hint(problem string) {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return
	}
	line := fmt.Sprintf("mootd: %s, run \"mootd status\"", problem)
	if opts, _ := terminalOptions(); opts.Colour != render.Plain {
		line = "\x1b[2m" + line + "\x1b[0m"
	}
	fmt.Println(line)
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

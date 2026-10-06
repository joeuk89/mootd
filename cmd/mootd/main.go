package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"time"

	"golang.org/x/term"

	"github.com/joeuk89/mootd/internal/greeting"
	"github.com/joeuk89/mootd/internal/render"
	"github.com/joeuk89/mootd/internal/store"
)

const expiryDays = 3

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
	case "import":
		err = importBatch(os.Args[2:])
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
	opts, rows := terminalOptions()
	st, err := store.Open()
	if err != nil {
		return err
	}

	var picked *greeting.Greeting
	err = st.Update(func(p *store.Pool) error {
		now := time.Now()
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
	if err != nil || picked == nil {
		return err
	}
	fmt.Print(render.Render(*picked, opts))
	return nil
}

func terminalOptions() (render.Options, int) {
	opts := render.Options{Width: 80, ShowSource: true}
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

// importBatch loads the kept greetings from a batch file written by the prototype generator.
func importBatch(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: mootd import <batch.json>")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	var batch struct {
		GeneratedAt string `json:"generated_at"`
		Greetings   []struct {
			greeting.Greeting
			Kept bool `json:"kept"`
		} `json:"greetings"`
	}
	if err := json.Unmarshal(data, &batch); err != nil {
		return err
	}
	added, err := time.ParseInLocation("2006-01-02T15:04:05", batch.GeneratedAt, time.Local)
	if err != nil {
		return fmt.Errorf("batch has no valid generated_at: %w", err)
	}

	var greetings []greeting.Greeting
	invalid := 0
	for _, item := range batch.Greetings {
		if !item.Kept {
			continue
		}
		g := item.Greeting
		g.Normalise()
		if err := g.Validate(); err != nil {
			invalid++
			continue
		}
		g.ID = greeting.NewID(added.Format("2006-01-02"), g)
		greetings = append(greetings, g)
	}

	st, err := store.Open()
	if err != nil {
		return err
	}
	n, err := st.AddBatch(greetings, added, expiryDays)
	if err != nil {
		return err
	}
	fmt.Printf("added %d, already in the pool %d, invalid %d\n", n, len(greetings)-n, invalid)
	return nil
}

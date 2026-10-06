package main

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/joeuk89/mootd/internal/greeting"
	"github.com/joeuk89/mootd/internal/store"
)

const maxReasonChars = 200

var errNoGreeting = errors.New("no greeting has been shown in this window")

// windowEntry finds the greeting this terminal window last showed. A greeting that
// has left the pool, because it expired or was noped, comes back from the archive.
func windowEntry(st *store.Store, p *store.Pool) (entry *store.Entry, inPool bool, err error) {
	window := store.WindowKey()
	if window == "" {
		return nil, false, errors.New("this command must run in a terminal window")
	}
	id := p.Windows[window]
	if id == "" {
		return nil, false, errNoGreeting
	}
	if e := p.Find(id); e != nil {
		return e, true, nil
	}
	archived, ok, err := st.Archived(id)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, errNoGreeting
	}
	return &archived, false, nil
}

func keep() error {
	st, err := store.Open()
	if err != nil {
		return err
	}
	var kept greeting.Greeting
	alreadyKept := false
	err = st.Update(func(p *store.Pool) error {
		entry, inPool, err := windowEntry(st, p)
		if err != nil {
			return err
		}
		if !inPool {
			p.Entries = append(p.Entries, *entry)
			entry = &p.Entries[len(p.Entries)-1]
		}
		alreadyKept = entry.Kept
		entry.Kept, entry.Expires = true, ""
		kept = entry.Greeting
		return nil
	})
	if err != nil {
		return err
	}
	if !alreadyKept {
		if err := st.AddFeedback(store.Feedback{Verdict: store.Keep, At: time.Now(), Greeting: kept}); err != nil {
			return err
		}
	}
	fmt.Printf("Kept for good: %s\n", excerpt(kept.Message))
	return nil
}

func nope(reason string) error {
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > maxReasonChars {
		return fmt.Errorf("keep the reason under %d characters", maxReasonChars)
	}
	st, err := store.Open()
	if err != nil {
		return err
	}
	var noped greeting.Greeting
	err = st.Update(func(p *store.Pool) error {
		entry, _, err := windowEntry(st, p)
		if err != nil {
			return err
		}
		noped = entry.Greeting
		p.Remove(noped.ID)
		return nil
	})
	if err != nil {
		return err
	}
	err = st.AddFeedback(store.Feedback{Verdict: store.Nope, Reason: reason, At: time.Now(), Greeting: noped})
	if err != nil {
		return err
	}
	fmt.Printf("Dropped: %s\n", excerpt(noped.Message))
	return nil
}

func openSource() error {
	st, err := store.Open()
	if err != nil {
		return err
	}
	var source *greeting.Source
	err = st.Update(func(p *store.Pool) error {
		entry, _, err := windowEntry(st, p)
		if err != nil {
			return err
		}
		source = entry.Greeting.Source
		return nil
	})
	if err != nil {
		return err
	}
	if source == nil {
		return errors.New("this greeting has no news story behind it")
	}
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.Command(opener, source.URL).Run()
}

func excerpt(message string) string {
	const limit = 60
	line := strings.Join(strings.Fields(message), " ")
	if runes := []rune(line); len(runes) > limit {
		line = string(runes[:limit]) + "…"
	}
	return line
}

package store

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joeuk89/mootd/internal/greeting"
)

var now = time.Date(2026, 10, 6, 9, 0, 0, 0, time.Local)

func noShuffle(int, func(i, j int)) {}

func anyFits(greeting.Greeting) bool { return true }

func entry(id, added, expires string) Entry {
	return Entry{Greeting: greeting.Greeting{ID: id}, Added: added, Expires: expires}
}

func TestPickOrder(t *testing.T) {
	shownEarlier := entry("old-evergreen-shown", "2026-09-01", "")
	shownEarlier.LastShown = now.Add(-time.Hour)
	pool := &Pool{Entries: []Entry{
		shownEarlier,
		entry("old-evergreen-unseen", "2026-09-01", ""),
		entry("yesterday-topical", "2026-10-05", "2026-10-08"),
		entry("expired-topical", "2026-10-01", "2026-10-04"),
		entry("today-topical", "2026-10-06", "2026-10-09"),
	}}

	want := []string{
		"today-topical",
		"yesterday-topical",
		"old-evergreen-unseen",
		"old-evergreen-shown",
		"today-topical",
	}
	for i, id := range want {
		at := now.Add(time.Duration(i) * time.Minute)
		got := pool.Pick(at, anyFits, noShuffle)
		if got == nil || got.Greeting.ID != id {
			t.Fatalf("pick %d = %v, want %s", i+1, got, id)
		}
		got.LastShown = at
	}
}

func TestPickPrefersTodaysEvergreenOverOlderTopical(t *testing.T) {
	pool := &Pool{Entries: []Entry{
		entry("yesterday-topical", "2026-10-05", "2026-10-08"),
		entry("today-evergreen", "2026-10-06", ""),
	}}
	if got := pool.Pick(now, anyFits, noShuffle); got.Greeting.ID != "today-evergreen" {
		t.Errorf("picked %s", got.Greeting.ID)
	}
}

func TestPickSkipsGreetingsThatDoNotFit(t *testing.T) {
	pool := &Pool{Entries: []Entry{
		entry("big", "2026-10-06", ""),
		entry("small", "2026-09-01", ""),
	}}
	fits := func(g greeting.Greeting) bool { return g.ID == "small" }
	if got := pool.Pick(now, fits, noShuffle); got.Greeting.ID != "small" {
		t.Errorf("picked %s", got.Greeting.ID)
	}
	if got := pool.Pick(now, func(greeting.Greeting) bool { return false }, noShuffle); got != nil {
		t.Errorf("picked %s when nothing fits", got.Greeting.ID)
	}
}

func TestPrune(t *testing.T) {
	pool := &Pool{Entries: []Entry{
		entry("evergreen", "2026-01-01", ""),
		entry("expires-today", "2026-10-03", "2026-10-06"),
		entry("expires-tomorrow", "2026-10-04", "2026-10-07"),
	}}
	pool.Prune(now)
	if len(pool.Entries) != 2 || pool.Entries[0].Greeting.ID != "evergreen" || pool.Entries[1].Greeting.ID != "expires-tomorrow" {
		t.Errorf("after prune: %+v", pool.Entries)
	}
}

func TestAddBatchAndUpdateRoundTrip(t *testing.T) {
	s := &Store{dir: t.TempDir()}
	batch := []greeting.Greeting{
		{ID: "a", Category: "uk"},
		{ID: "b", Category: greeting.Evergreen},
	}

	if n, err := s.AddBatch(batch, now, 3); err != nil || n != 2 {
		t.Fatalf("first add: n=%d err=%v", n, err)
	}
	if n, err := s.AddBatch(batch, now, 3); err != nil || n != 0 {
		t.Fatalf("second add should skip both: n=%d err=%v", n, err)
	}

	err := s.Update(func(p *Pool) error {
		if len(p.Entries) != 2 {
			t.Fatalf("pool has %d entries", len(p.Entries))
		}
		if got := p.Entries[0].Expires; got != "2026-10-09" {
			t.Errorf("topical expires %q, want 2026-10-09", got)
		}
		if got := p.Entries[1].Expires; got != "" {
			t.Errorf("evergreen expires %q, want never", got)
		}
		p.Entries[0].LastShown = now
		p.SetWindow("42:100", "b")
		p.SetWindow("7:300", "b")
		p.SetWindow("42:200", "a")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = s.Update(func(p *Pool) error {
		if !p.Entries[0].LastShown.Equal(now) {
			t.Errorf("last shown not saved: %v", p.Entries[0].LastShown)
		}
		if !p.Entries[1].LastShown.IsZero() {
			t.Errorf("unshown entry has a last-shown time: %v", p.Entries[1].LastShown)
		}
		if len(p.Windows) != 2 || p.Windows["42:200"] != "a" || p.Windows["7:300"] != "b" {
			t.Errorf("a window should replace earlier ones on the same device only: %v", p.Windows)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(filepath.Join(s.dir, "archive.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	lines := 0
	for sc := bufio.NewScanner(f); sc.Scan(); {
		lines++
	}
	if lines != 2 {
		t.Errorf("archive has %d lines, want 2", lines)
	}
}

func TestGenerationDue(t *testing.T) {
	var g Generation
	if !g.Due(now) {
		t.Error("a fresh install should be due")
	}

	g.LastAttempt = now
	if g.Due(now.Add(59 * time.Minute)) {
		t.Error("should wait an hour between attempts")
	}
	if !g.Due(now.Add(time.Hour)) {
		t.Error("should retry after an hour")
	}

	g.Record(now, errors.New("offline"))
	g.Record(now.Add(time.Hour), errors.New("still offline"))
	if !g.FailingSince.Equal(now) || g.LastError != "still offline" {
		t.Errorf("after two failures: %+v", g)
	}

	g.Record(now.Add(2*time.Hour), nil)
	if !g.FailingSince.IsZero() || g.LastError != "" {
		t.Errorf("a success should clear the failure: %+v", g)
	}
	if g.Due(now.Add(10 * time.Hour)) {
		t.Error("should not be due again on the day it succeeded")
	}
	if !g.Due(now.Add(24 * time.Hour)) {
		t.Error("should be due the next day")
	}
}

func TestRecentMessages(t *testing.T) {
	s := &Store{dir: t.TempDir()}
	if got, err := s.RecentMessages(5); err != nil || got != nil {
		t.Fatalf("empty store: %v, %v", got, err)
	}
	batch := []greeting.Greeting{{ID: "a", Message: "one"}, {ID: "b", Message: "two"}, {ID: "c", Message: "three"}}
	if _, err := s.AddBatch(batch, now, 3); err != nil {
		t.Fatal(err)
	}
	got, err := s.RecentMessages(2)
	if err != nil || len(got) != 2 || got[0] != "two" || got[1] != "three" {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestLockGenerationAllowsOneHolder(t *testing.T) {
	s := &Store{dir: t.TempDir()}
	release, ok, err := s.LockGeneration()
	if err != nil || !ok {
		t.Fatalf("first lock: ok=%v err=%v", ok, err)
	}
	if _, ok, err := s.LockGeneration(); err != nil || ok {
		t.Fatalf("second lock should be refused: ok=%v err=%v", ok, err)
	}
	release()
	if release, ok, _ := s.LockGeneration(); !ok {
		t.Fatal("lock should be free after release")
	} else {
		release()
	}
}

func TestOpenLogDeletesOldLogs(t *testing.T) {
	s := &Store{dir: t.TempDir()}
	logs := filepath.Join(s.dir, "logs")
	os.MkdirAll(logs, 0o755)
	for _, name := range []string{"2026-09-21.log", "2026-09-22.log", "2026-10-05.log"} {
		os.WriteFile(filepath.Join(logs, name), nil, 0o644)
	}

	f, err := s.OpenLog(now)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	entries, _ := os.ReadDir(logs)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	want := "2026-09-22.log 2026-10-05.log 2026-10-06.log"
	if got := strings.Join(names, " "); got != want {
		t.Errorf("logs = %s, want %s", got, want)
	}
}

func TestRecentFeedbackUsesTheLatestVerdictPerGreeting(t *testing.T) {
	s := &Store{dir: t.TempDir()}
	add := func(verdict, id, reason string) {
		t.Helper()
		f := Feedback{Verdict: verdict, Reason: reason, At: now, Greeting: greeting.Greeting{ID: id}}
		if err := s.AddFeedback(f); err != nil {
			t.Fatal(err)
		}
	}
	add(Keep, "a", "")
	add(Keep, "b", "")
	add(Nope, "c", "too long")
	add(Keep, "d", "")
	add(Nope, "a", "changed my mind")

	kept, noped, err := s.RecentFeedback(10)
	if err != nil {
		t.Fatal(err)
	}
	ids := func(items []Feedback) string {
		var out []string
		for _, f := range items {
			out = append(out, f.Greeting.ID)
		}
		return strings.Join(out, " ")
	}
	if ids(kept) != "d b" || ids(noped) != "a c" {
		t.Errorf("kept = %q, noped = %q", ids(kept), ids(noped))
	}
	if noped[0].Reason != "changed my mind" {
		t.Errorf("reason = %q", noped[0].Reason)
	}

	kept, noped, _ = s.RecentFeedback(1)
	if ids(kept) != "d" || ids(noped) != "a" {
		t.Errorf("with a limit of 1: kept = %q, noped = %q", ids(kept), ids(noped))
	}
}

func TestArchivedFindsGreetingsThatLeftThePool(t *testing.T) {
	s := &Store{dir: t.TempDir()}
	if _, err := s.AddBatch([]greeting.Greeting{{ID: "a", Category: "uk", Message: "hello"}}, now, 3); err != nil {
		t.Fatal(err)
	}
	err := s.Update(func(p *Pool) error {
		if p.Find("a") == nil {
			t.Error("Find should see the new greeting")
		}
		p.Remove("a")
		if p.Find("a") != nil {
			t.Error("Find should not see a removed greeting")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	entry, ok, err := s.Archived("a")
	if err != nil || !ok || entry.Greeting.Message != "hello" || entry.Expires != "2026-10-09" {
		t.Errorf("archived = %+v, ok=%v, err=%v", entry, ok, err)
	}
	if _, ok, _ := s.Archived("missing"); ok {
		t.Error("found a greeting that was never added")
	}
}

func TestFailingFor(t *testing.T) {
	var g Generation
	if g.FailingFor(now) != 0 {
		t.Error("a fresh install is not failing")
	}
	g.Record(now, errors.New("offline"))
	if got := g.FailingFor(now.Add(72 * time.Hour)); got != 72*time.Hour {
		t.Errorf("failing for %v", got)
	}
	g.Record(now, nil)
	if g.FailingFor(now.Add(time.Hour)) != 0 {
		t.Error("a success should stop the clock")
	}
}

package store

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"github.com/joeuk89/mootd/internal/greeting"
)

const (
	dateLayout    = "2006-01-02"
	retryInterval = time.Hour
	// builtinAdded stands in for the added date of a greeting that shipped with the program.
	builtinAdded = "builtin"
	logDays      = 14
)

type Entry struct {
	Greeting greeting.Greeting `json:"greeting"`
	Added    string            `json:"added"`
	// Expires is the first date the greeting is no longer shown. Empty means never.
	Expires   string    `json:"expires,omitempty"`
	Kept      bool      `json:"kept,omitempty"`
	LastShown time.Time `json:"last_shown,omitzero"`
}

const (
	Keep = "keep"
	Nope = "nope"
)

type Feedback struct {
	Verdict  string            `json:"verdict"`
	Reason   string            `json:"reason,omitempty"`
	At       time.Time         `json:"at"`
	Greeting greeting.Greeting `json:"greeting"`
}

type Pool struct {
	Entries []Entry `json:"entries"`
	// Windows maps a terminal window to the ID of the greeting it last showed.
	Windows    map[string]string `json:"windows,omitempty"`
	Generation Generation        `json:"generation,omitzero"`
	// Builtins is the version of the built-in greetings already added to this pool.
	Builtins int `json:"builtins,omitempty"`
}

type Generation struct {
	LastAttempt time.Time `json:"last_attempt,omitzero"`
	LastSuccess time.Time `json:"last_success,omitzero"`
	// FailingSince is when the current run of failures began. Zero means the last run worked.
	FailingSince time.Time `json:"failing_since,omitzero"`
	LastError    string    `json:"last_error,omitempty"`
}

// FailingFor reports how long generation has been failing, or zero if the last run worked.
func (g Generation) FailingFor(now time.Time) time.Duration {
	if g.FailingSince.IsZero() {
		return 0
	}
	return now.Sub(g.FailingSince)
}

// Due reports whether today still needs a batch and the last attempt is old enough to retry.
func (g Generation) Due(now time.Time) bool {
	doneToday := g.LastSuccess.Format(dateLayout) == now.Format(dateLayout)
	return !doneToday && now.Sub(g.LastAttempt) >= retryInterval
}

func (g *Generation) Record(now time.Time, err error) {
	if err == nil {
		g.LastSuccess, g.FailingSince, g.LastError = now, time.Time{}, ""
		return
	}
	if g.FailingSince.IsZero() {
		g.FailingSince = now
	}
	g.LastError = err.Error()
}

type Store struct{ dir string }

func Open() (*Store, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return OpenAt(filepath.Join(base, "mootd"))
}

func OpenAt(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Dir() string { return s.dir }

// Update runs fn on the pool under an exclusive lock and saves the result, so
// several terminals opening at once cannot lose each other's changes.
func (s *Store) Update(fn func(*Pool) error) error {
	lock, err := os.OpenFile(filepath.Join(s.dir, "pool.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}

	pool, err := s.load()
	if err != nil {
		return err
	}
	if err := fn(pool); err != nil {
		return err
	}
	return s.save(pool)
}

func (s *Store) poolPath() string { return filepath.Join(s.dir, "pool.json") }

func (s *Store) load() (*Pool, error) {
	pool := &Pool{}
	data, err := os.ReadFile(s.poolPath())
	if errors.Is(err, fs.ErrNotExist) {
		return pool, nil
	}
	if err != nil {
		return nil, err
	}
	return pool, json.Unmarshal(data, pool)
}

func (s *Store) save(pool *Pool) error {
	data, err := json.Marshal(pool)
	if err != nil {
		return err
	}
	tmp := s.poolPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.poolPath())
}

// AddBatch adds greetings to the pool and the archive, skipping IDs the pool already
// holds. Topical greetings expire after expiryDays; evergreen ones never do.
func (s *Store) AddBatch(greetings []greeting.Greeting, added time.Time, expiryDays int) (int, error) {
	var fresh []Entry
	err := s.Update(func(p *Pool) error {
		known := map[string]bool{}
		for _, e := range p.Entries {
			known[e.Greeting.ID] = true
		}
		for _, g := range greetings {
			if known[g.ID] {
				continue
			}
			known[g.ID] = true
			entry := Entry{Greeting: g, Added: added.Format(dateLayout)}
			if g.Category != greeting.Evergreen {
				entry.Expires = added.AddDate(0, 0, expiryDays).Format(dateLayout)
			}
			fresh = append(fresh, entry)
		}
		p.Entries = append(p.Entries, fresh...)
		return s.archive(fresh)
	})
	return len(fresh), err
}

func (s *Store) archive(entries []Entry) error {
	return appendLines(filepath.Join(s.dir, "archive.jsonl"), entries)
}

// SeedBuiltins adds the greetings that ship with the program. It skips any the
// archive has seen before, so a built-in the user dropped stays gone after an upgrade.
// Built-ins rank below fresh greetings: they are what shows when nothing newer is left.
func (s *Store) SeedBuiltins(p *Pool, version int, greetings []greeting.Greeting) error {
	archived, err := readLines[Entry](filepath.Join(s.dir, "archive.jsonl"))
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, e := range archived {
		known[e.Greeting.ID] = true
	}
	var fresh []Entry
	for _, g := range greetings {
		if !known[g.ID] {
			fresh = append(fresh, Entry{Greeting: g, Added: builtinAdded})
		}
	}
	p.Entries = append(p.Entries, fresh...)
	p.Builtins = version
	return s.archive(fresh)
}

// Archived finds a greeting that was once added, even if it has since expired or been removed.
func (s *Store) Archived(id string) (Entry, bool, error) {
	entries, err := readLines[Entry](filepath.Join(s.dir, "archive.jsonl"))
	for _, e := range entries {
		if e.Greeting.ID == id {
			return e, true, err
		}
	}
	return Entry{}, false, err
}

func (s *Store) AddFeedback(f Feedback) error {
	return appendLines(filepath.Join(s.dir, "feedback.jsonl"), []Feedback{f})
}

// RecentFeedback returns up to n kept and n noped greetings, newest first. Only the
// latest verdict on each greeting counts.
func (s *Store) RecentFeedback(n int) (kept, noped []Feedback, err error) {
	all, err := readLines[Feedback](filepath.Join(s.dir, "feedback.jsonl"))
	seen := map[string]bool{}
	for _, f := range slices.Backward(all) {
		if seen[f.Greeting.ID] {
			continue
		}
		seen[f.Greeting.ID] = true
		switch {
		case f.Verdict == Keep && len(kept) < n:
			kept = append(kept, f)
		case f.Verdict == Nope && len(noped) < n:
			noped = append(noped, f)
		}
	}
	return kept, noped, err
}

func appendLines[T any](path string, items []T) error {
	if len(items) == 0 {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, item := range items {
		if err := enc.Encode(item); err != nil {
			return err
		}
	}
	return nil
}

// readLines reads a file of one JSON value per line, skipping lines it cannot parse.
// A missing file reads as empty.
func readLines[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []T
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var item T
		if json.Unmarshal(sc.Bytes(), &item) == nil {
			out = append(out, item)
		}
	}
	return out, sc.Err()
}

// RecentMessages returns the messages of the last n greetings added, oldest first.
func (s *Store) RecentMessages(n int) ([]string, error) {
	entries, err := readLines[Entry](filepath.Join(s.dir, "archive.jsonl"))
	if len(entries) > n {
		entries = entries[len(entries)-n:]
	}
	var messages []string
	for _, e := range entries {
		messages = append(messages, e.Greeting.Message)
	}
	return messages, err
}

// GenerationRunning reports whether another process holds the generation lock.
func (s *Store) GenerationRunning() bool {
	release, ok, err := s.LockGeneration()
	if err != nil {
		return false
	}
	if ok {
		release()
	}
	return !ok
}

// LatestLog returns the path of the newest log file, or "" if there is none.
func (s *Store) LatestLog() string {
	entries, err := os.ReadDir(filepath.Join(s.dir, "logs"))
	if err != nil || len(entries) == 0 {
		return ""
	}
	return filepath.Join(s.dir, "logs", entries[len(entries)-1].Name())
}

// LockGeneration takes the lock that stops two generations running at once. It
// reports false, without waiting, when another process holds it.
func (s *Store) LockGeneration() (release func(), ok bool, err error) {
	f, err := os.OpenFile(filepath.Join(s.dir, "generate.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() { f.Close() }, true, nil
}

// OpenLog opens today's log file for appending and deletes logs older than 14 days.
func (s *Store) OpenLog(now time.Time) (*os.File, error) {
	dir := filepath.Join(s.dir, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	oldest := now.AddDate(0, 0, -logDays).Format(dateLayout) + ".log"
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.Name() < oldest {
				os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	return os.OpenFile(filepath.Join(dir, now.Format(dateLayout)+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

// Prune drops expired greetings, then the oldest evergreen ones beyond evergreenLimit.
// Kept and built-in greetings do not count towards the limit and are never dropped.
func (p *Pool) Prune(now time.Time, evergreenLimit int) {
	today := now.Format(dateLayout)
	p.Entries = slices.DeleteFunc(p.Entries, func(e Entry) bool { return e.expired(today) })

	excess := -evergreenLimit
	for _, e := range p.Entries {
		if e.limited() {
			excess++
		}
	}
	// Entries are stored oldest first, so the first ones found are the ones to drop.
	p.Entries = slices.DeleteFunc(p.Entries, func(e Entry) bool {
		if excess > 0 && e.limited() {
			excess--
			return true
		}
		return false
	})
}

// Pick chooses the next greeting to show: today's unseen first, then unseen topical
// ones from earlier days, then whatever was shown longest ago. It returns nil when
// nothing fits.
func (p *Pool) Pick(now time.Time, fits func(greeting.Greeting) bool, shuffle func(n int, swap func(i, j int))) *Entry {
	today := now.Format(dateLayout)
	var candidates []*Entry
	for i := range p.Entries {
		if e := &p.Entries[i]; !e.expired(today) && fits(e.Greeting) {
			candidates = append(candidates, e)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
	slices.SortStableFunc(candidates, func(a, b *Entry) int {
		if c := cmp.Compare(a.tier(today), b.tier(today)); c != 0 {
			return c
		}
		return a.LastShown.Compare(b.LastShown)
	})
	return candidates[0]
}

func (p *Pool) Find(id string) *Entry {
	for i := range p.Entries {
		if p.Entries[i].Greeting.ID == id {
			return &p.Entries[i]
		}
	}
	return nil
}

func (p *Pool) Remove(id string) {
	p.Entries = slices.DeleteFunc(p.Entries, func(e Entry) bool { return e.Greeting.ID == id })
}

// SetWindow records which greeting a window shows. It forgets earlier windows on the
// same terminal device: the device is only reused once those windows have closed.
func (p *Pool) SetWindow(window, id string) {
	if p.Windows == nil {
		p.Windows = map[string]string{}
	}
	device, _, _ := strings.Cut(window, ":")
	for other := range p.Windows {
		if strings.HasPrefix(other, device+":") {
			delete(p.Windows, other)
		}
	}
	p.Windows[window] = id
}

// limited reports whether the entry is a generated evergreen greeting, the only kind
// the evergreen limit applies to.
func (e *Entry) limited() bool {
	return e.Expires == "" && !e.Kept && e.Added != builtinAdded
}

func (e *Entry) expired(today string) bool {
	return e.Expires != "" && today >= e.Expires
}

func (e *Entry) tier(today string) int {
	switch {
	case !e.LastShown.IsZero():
		return 2
	case e.Added == today:
		return 0
	case e.Expires != "":
		return 1
	default:
		return 2
	}
}

// WindowKey identifies the terminal window this process runs in, as "device:session".
// The device number alone is not enough, because the system hands a closed window's
// device to the next window opened; the session ID tells the two apart. It returns ""
// when no terminal is attached.
func WindowKey() string {
	for _, f := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
		fd := int(f.Fd())
		if !term.IsTerminal(fd) {
			continue
		}
		var st syscall.Stat_t
		if err := syscall.Fstat(fd, &st); err != nil {
			continue
		}
		session, err := unix.Getsid(0)
		if err != nil {
			continue
		}
		return strconv.FormatUint(uint64(st.Rdev), 10) + ":" + strconv.Itoa(session)
	}
	return ""
}

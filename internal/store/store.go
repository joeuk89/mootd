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
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/joeuk89/mootd/internal/greeting"
)

const (
	dateLayout    = "2006-01-02"
	retryInterval = time.Hour
	logDays       = 14
)

type Entry struct {
	Greeting greeting.Greeting `json:"greeting"`
	Added    string            `json:"added"`
	// Expires is the first date the greeting is no longer shown. Empty means never.
	Expires   string    `json:"expires,omitempty"`
	LastShown time.Time `json:"last_shown,omitzero"`
}

type Pool struct {
	Entries []Entry `json:"entries"`
	// Windows maps a terminal window to the ID of the greeting it last showed.
	Windows    map[string]string `json:"windows,omitempty"`
	Generation Generation        `json:"generation,omitzero"`
}

type Generation struct {
	LastAttempt time.Time `json:"last_attempt,omitzero"`
	LastSuccess time.Time `json:"last_success,omitzero"`
	// FailingSince is when the current run of failures began. Zero means the last run worked.
	FailingSince time.Time `json:"failing_since,omitzero"`
	LastError    string    `json:"last_error,omitempty"`
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
	if len(entries) == 0 {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(s.dir, "archive.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

// RecentMessages returns the messages of the last n greetings added, oldest first.
func (s *Store) RecentMessages(n int) ([]string, error) {
	f, err := os.Open(filepath.Join(s.dir, "archive.jsonl"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var messages []string
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			messages = append(messages, e.Greeting.Message)
		}
	}
	if len(messages) > n {
		messages = messages[len(messages)-n:]
	}
	return messages, sc.Err()
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

func (p *Pool) Prune(now time.Time) {
	today := now.Format(dateLayout)
	p.Entries = slices.DeleteFunc(p.Entries, func(e Entry) bool { return e.expired(today) })
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

func (p *Pool) SetWindow(window, id string) {
	if p.Windows == nil {
		p.Windows = map[string]string{}
	}
	p.Windows[window] = id
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

// WindowKey identifies the terminal window this process runs in by the device number
// of its terminal. It returns "" when no terminal is attached.
func WindowKey() string {
	for _, f := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
		fd := int(f.Fd())
		if !term.IsTerminal(fd) {
			continue
		}
		var st syscall.Stat_t
		if err := syscall.Fstat(fd, &st); err == nil {
			return strconv.FormatUint(uint64(st.Rdev), 10)
		}
	}
	return ""
}

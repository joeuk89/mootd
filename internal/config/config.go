package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/joeuk89/mootd/internal/greeting"
)

type Config struct {
	Model  string `toml:"model"`
	Effort string `toml:"effort"`
	// Cull asks for half as many greetings again, then has a second call score them and keeps the best.
	Cull            bool     `toml:"cull"`
	ExpiryDays      int      `toml:"expiry_days"`
	EvergreenLimit  int      `toml:"evergreen_limit"`
	ShowSource      bool     `toml:"show_source"`
	SkipTerminals   []string `toml:"skip_terminals"`
	ClaudeBin       string   `toml:"claude_bin"`
	ClaudeConfigDir string   `toml:"claude_config_dir"`
	Style           string   `toml:"style"`
	Interests       string   `toml:"interests"`
	// Mix is how many greetings to keep per category in each daily batch.
	Mix   map[string]int `toml:"mix"`
	Feeds []Feed         `toml:"feeds"`
}

type Feed struct {
	Category string `toml:"category"`
	Outlet   string `toml:"outlet"`
	URL      string `toml:"url"`
}

func Default() Config {
	return Config{
		Model:      "claude-opus-5-5",
		Cull:       true,
		ExpiryDays: 3,
		// About seven weeks of evergreen greetings at the default mix.
		EvergreenLimit: 200,
		ShowSource:     true,
		Mix: map[string]int{
			"programming":      4,
			"international":    4,
			"culture":          4,
			"uk":               4,
			greeting.Evergreen: 4,
		},
		Feeds: []Feed{
			{"programming", "Hacker News", "https://hnrss.org/frontpage"},
			{"programming", "Lobsters", "https://lobste.rs/rss"},
			{"programming", "The Register", "https://www.theregister.com/headlines.atom"},
			{"international", "BBC News", "https://feeds.bbci.co.uk/news/world/rss.xml"},
			{"international", "The Guardian", "https://www.theguardian.com/world/rss"},
			{"culture", "The Guardian", "https://www.theguardian.com/culture/rss"},
			{"culture", "BBC News", "https://feeds.bbci.co.uk/news/entertainment_and_arts/rss.xml"},
			{"uk", "BBC News", "https://feeds.bbci.co.uk/news/uk/rss.xml"},
			{"uk", "The Guardian", "https://www.theguardian.com/uk-news/rss"},
		},
	}
}

func Path() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "mootd", "config.toml"), nil
}

func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	return LoadFile(path)
}

// LoadFile reads settings over the defaults. A missing file gives the defaults.
// A [mix] table replaces the default mix; a feeds list replaces the default feeds.
func LoadFile(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, err
	}

	file := cfg
	file.Mix, file.Feeds = nil, nil
	if _, err := toml.Decode(string(data), &file); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if file.Mix == nil {
		file.Mix = cfg.Mix
	}
	if file.Feeds == nil {
		file.Feeds = cfg.Feeds
	}
	cfg = file

	cfg.ClaudeBin = expandHome(cfg.ClaudeBin)
	cfg.ClaudeConfigDir = expandHome(cfg.ClaudeConfigDir)
	if err := cfg.validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.ExpiryDays < 1 {
		return errors.New("expiry_days must be at least 1")
	}
	if c.EvergreenLimit < 1 {
		return errors.New("evergreen_limit must be at least 1")
	}
	fed := map[string]bool{}
	for _, f := range c.Feeds {
		if f.Category == "" || f.Outlet == "" || f.URL == "" {
			return errors.New("every feed needs a category, an outlet and a url")
		}
		if f.Category == greeting.Evergreen {
			return fmt.Errorf("%q is reserved for greetings without a headline and cannot have feeds", greeting.Evergreen)
		}
		fed[f.Category] = true
	}
	total := 0
	for category, count := range c.Mix {
		if count < 0 {
			return fmt.Errorf("mix.%s must not be negative", category)
		}
		if count > 0 && category != greeting.Evergreen && !fed[category] {
			return fmt.Errorf("mix.%s has no feeds", category)
		}
		total += count
	}
	if total == 0 {
		return errors.New("mix asks for no greetings")
	}
	return nil
}

// Categories lists the categories the mix asks for: fed ones in feed order, then evergreen.
func (c Config) Categories() []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range c.Feeds {
		if !seen[f.Category] && c.Mix[f.Category] > 0 {
			out = append(out, f.Category)
		}
		seen[f.Category] = true
	}
	if c.Mix[greeting.Evergreen] > 0 {
		out = append(out, greeting.Evergreen)
	}
	return out
}

func expandHome(path string) string {
	rest, ok := strings.CutPrefix(path, "~/")
	if !ok {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, rest)
}

// Create writes the commented template to path unless a file is already there.
func Create(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(Template())
	return err
}

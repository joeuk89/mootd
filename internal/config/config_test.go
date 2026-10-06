package config

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMissingFileGivesDefaults(t *testing.T) {
	cfg, err := LoadFile(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "claude-opus-5-5" || !cfg.Cull || cfg.ExpiryDays != 3 || !cfg.ShowSource {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	want := []string{"programming", "international", "culture", "uk", "evergreen"}
	if got := cfg.Categories(); !slices.Equal(got, want) {
		t.Errorf("categories = %v, want %v", got, want)
	}
}

func TestFileOverridesDefaults(t *testing.T) {
	t.Setenv("HOME", "/home/someone")
	cfg, err := LoadFile(write(t, `
model = "claude-sonnet-5-5"
effort = "low"
cull = false
show_source = false
skip_terminals = ["vscode"]
claude_config_dir = "~/.claude-personal"
style = "dry"
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "claude-sonnet-5-5" || cfg.Effort != "low" || cfg.Cull || cfg.ShowSource || cfg.Style != "dry" {
		t.Errorf("settings not applied: %+v", cfg)
	}
	if cfg.ClaudeConfigDir != "/home/someone/.claude-personal" {
		t.Errorf("claude_config_dir = %q", cfg.ClaudeConfigDir)
	}
	if cfg.ExpiryDays != 3 || len(cfg.Feeds) != 9 || cfg.Mix["uk"] != 4 {
		t.Errorf("unset settings lost their defaults: %+v", cfg)
	}
}

func TestMixAndFeedsReplaceTheDefaults(t *testing.T) {
	cfg, err := LoadFile(write(t, `
[mix]
football = 6
evergreen = 2

[[feeds]]
category = "football"
outlet = "BBC Sport"
url = "https://example.com/football.xml"
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Mix) != 2 || cfg.Mix["football"] != 6 || len(cfg.Feeds) != 1 {
		t.Errorf("mix = %v, feeds = %v", cfg.Mix, cfg.Feeds)
	}
	if got, want := cfg.Categories(), []string{"football", "evergreen"}; !slices.Equal(got, want) {
		t.Errorf("categories = %v, want %v", got, want)
	}
}

func TestInvalidConfig(t *testing.T) {
	tests := map[string]string{
		"expiry_days = 0":            "expiry_days",
		"[mix]\nfootball = 3":        "mix.football has no feeds",
		"[mix]\nuk = -1":             "must not be negative",
		"[mix]\nuk = 0":              "asks for no greetings",
		"model = ":                   "config.toml",
		"[[feeds]]\ncategory = 'uk'": "every feed needs",
		"[[feeds]]\ncategory = 'evergreen'\noutlet = 'x'\nurl = 'https://example.com'": "reserved",
	}
	for body, want := range tests {
		_, err := LoadFile(write(t, body))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("config %q: error %v, want one containing %q", body, err, want)
		}
	}
}

func TestTemplateIsAllCommentsAndUncommentsToTheDefaults(t *testing.T) {
	asWritten, err := LoadFile(write(t, Template()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(asWritten, Default()) {
		t.Errorf("the untouched template should change nothing")
	}

	uncommented := regexp.MustCompile(`(?m)^#(\S)`).ReplaceAllString(Template(), "$1")
	got, err := LoadFile(write(t, uncommented))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.SkipTerminals) == 0 {
		got.SkipTerminals = nil
	}
	if !reflect.DeepEqual(got, Default()) {
		t.Errorf("uncommented template differs from the defaults:\n got %+v\nwant %+v", got, Default())
	}
}

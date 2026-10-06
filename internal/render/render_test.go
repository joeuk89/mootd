package render

import (
	"strings"
	"testing"

	"github.com/joeuk89/mootd/internal/greeting"
)

func cat() greeting.Greeting {
	return greeting.Greeting{
		Art:           []string{" /\\_/\\", "( o.o )", " > ^ <", " /   \\"},
		SpeakerCol:    3,
		Colour:        greeting.Colour{Mode: "lines", Lines: []string{"#ff0000", "#00ff00", "#0000ff", "#808080"}},
		Message:       "Hello there",
		MessageColour: "#ffffff",
		Source:        &greeting.Source{Context: "A cat spoke", Outlet: "BBC News", URL: "https://example.com/cat"},
	}
}

func TestRenderPlain(t *testing.T) {
	want := strings.Join([]string{
		"",
		"  ╭─────────────╮",
		"  │ Hello there │",
		"  ╰──┬──────────╯",
		"     │",
		"   /\\_/\\",
		"  ( o.o )",
		"   > ^ <",
		"   /   \\",
		"",
		"  re: A cat spoke · BBC News",
		"",
		"",
	}, "\n")
	if got := Render(cat(), Options{Width: 80, ShowSource: true}); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderWithoutSource(t *testing.T) {
	got := Render(cat(), Options{Width: 80})
	if strings.Contains(got, "re:") {
		t.Errorf("source line shown when ShowSource is off:\n%s", got)
	}
}

func TestRenderColour(t *testing.T) {
	got := Render(cat(), Options{Width: 80, Colour: TrueColour, Hyperlinks: true, ShowSource: true})
	for _, want := range []string{
		"│ \x1b[38;2;255;255;255mHello there\x1b[0m │",
		"\x1b[38;2;255;0;0m /\\_/\\\x1b[0m",
		"\x1b]8;;https://example.com/cat\x1b\\\x1b[2mre: A cat spoke · BBC News\x1b[0m\x1b]8;;\x1b\\",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output is missing %q", want)
		}
	}

	got = Render(cat(), Options{Width: 80, Colour: Colour256})
	if !strings.Contains(got, "\x1b[38;5;196m /\\_/\\\x1b[0m") {
		t.Errorf("256-colour output is missing the downgraded red:\n%q", got)
	}
}

func TestGradient(t *testing.T) {
	g := greeting.Greeting{
		Art:    []string{"a b", "c"},
		Colour: greeting.Colour{Mode: "gradient", Stops: []string{"#000000", "#ffffff"}, Direction: "horizontal"},
	}
	got := gradient(g, TrueColour)
	want := []string{
		"\x1b[38;2;0;0;0ma \x1b[38;2;255;255;255mb\x1b[0m",
		"\x1b[38;2;0;0;0mc\x1b[0m",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}

	g.Colour.Direction = "vertical"
	got = gradient(g, TrueColour)
	if want := "\x1b[38;2;255;255;255mc\x1b[0m"; got[1] != want {
		t.Errorf("vertical bottom line = %q, want %q", got[1], want)
	}
}

func TestBlendThreeStops(t *testing.T) {
	stops := []rgb{{0, 0, 0}, {100, 100, 100}, {200, 0, 0}}
	tests := map[float64]rgb{0: {0, 0, 0}, 0.25: {50, 50, 50}, 0.5: {100, 100, 100}, 1: {200, 0, 0}}
	for at, want := range tests {
		if got := blend(stops, at); got != want {
			t.Errorf("blend(%v) = %v, want %v", at, got, want)
		}
	}
}

func TestXterm256(t *testing.T) {
	tests := map[string]int{"#ff0000": 196, "#000000": 16, "#ffffff": 231, "#808080": 244, "#0000ff": 21}
	for hex, want := range tests {
		c, _ := parseHex(hex)
		if got := c.xterm256(); got != want {
			t.Errorf("%s = %d, want %d", hex, got, want)
		}
	}
}

func TestWrap(t *testing.T) {
	tests := []struct {
		text  string
		limit int
		want  []string
	}{
		{"short", 20, []string{"short"}},
		{"one two three four", 9, []string{"one two", "three", "four"}},
		{"first line\nsecond line", 20, []string{"first line", "second line"}},
		{"abcdefghij", 4, []string{"abcd", "efgh", "ij"}},
		{"hi abcdefgh", 4, []string{"hi", "abcd", "efgh"}},
	}
	for _, tt := range tests {
		got := wrap(tt.text, tt.limit)
		if strings.Join(got, "|") != strings.Join(tt.want, "|") {
			t.Errorf("wrap(%q, %d) = %q, want %q", tt.text, tt.limit, got, tt.want)
		}
	}
}

func TestBubbleTailStaysInsideTheBubble(t *testing.T) {
	g := cat()
	g.Message = "Hi"

	g.SpeakerCol = 30
	lines := bubble(g, Options{Width: 80})
	if got, want := lines[2], "╰───┬╯"; got != want {
		t.Errorf("far-right tail = %q, want %q", got, want)
	}

	g.SpeakerCol = 0
	lines = bubble(g, Options{Width: 80})
	if got, want := lines[2], "╰┬───╯"; got != want {
		t.Errorf("far-left tail = %q, want %q", got, want)
	}
}

func TestSourceLineTruncates(t *testing.T) {
	got := sourceLine(*cat().Source, Options{Width: 16})
	if want := "re: A cat spo…"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFits(t *testing.T) {
	g := cat()
	opts := Options{Width: 80, ShowSource: true}
	if !Fits(g, opts, 24) {
		t.Error("should fit an 80x24 terminal")
	}
	if Fits(g, opts, 12) {
		t.Error("should not fit 12 rows: the greeting needs 10 plus 4 spare")
	}
	if Fits(g, Options{Width: 24, ShowSource: true}, 24) {
		t.Error("should not fit 24 columns: the bubble text would be under 20 wide")
	}

	g.Art[0] = strings.Repeat("#", 40)
	if Fits(g, Options{Width: 30, ShowSource: true}, 24) {
		t.Error("should not fit when the art is wider than the terminal")
	}
}

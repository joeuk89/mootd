package greeting

import (
	"strings"
	"testing"
)

func valid() Greeting {
	return Greeting{
		Category:      "uk",
		Art:           []string{" /\\_/\\", "( o.o )", " > ^ <", " /   \\"},
		Colour:        Colour{Mode: "gradient", Stops: []string{"#ff8800", "#3366ff"}, Direction: "vertical"},
		Message:       "Line one\nline two",
		MessageColour: "#44aa66",
		Source:        &Source{Context: "A cat does something", Outlet: "BBC News", URL: "https://example.com/a?b=1"},
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Greeting)
		wantErr string
	}{
		{"valid", func(g *Greeting) {}, ""},
		{"valid without source", func(g *Greeting) { g.Source = nil }, ""},
		{"valid line colours", func(g *Greeting) {
			g.Colour = Colour{Mode: "lines", Lines: []string{"#111111", "#222222", "#333333", "#444444"}}
		}, ""},
		{"box and block characters", func(g *Greeting) { g.Art[0] = "┌─┐ ███" }, ""},
		{"too few art lines", func(g *Greeting) { g.Art = g.Art[:3] }, "art has 3 lines"},
		{"art too wide", func(g *Greeting) { g.Art[0] = strings.Repeat("#", 41) }, "wider than"},
		{"emoji in art", func(g *Greeting) { g.Art[0] = "🐄" }, "disallowed character"},
		{"escape in art", func(g *Greeting) { g.Art[0] = "\x1b[31m" }, "disallowed character"},
		{"empty message", func(g *Greeting) { g.Message = "" }, "message is 0"},
		{"long message", func(g *Greeting) { g.Message = strings.Repeat("a", 241) }, "message is 241"},
		{"escape in message", func(g *Greeting) { g.Message = "hi \x1b]0;title\x07" }, "control characters"},
		{"bidi override in message", func(g *Greeting) { g.Message = "hi ‮ there" }, "control characters"},
		{"line colour count", func(g *Greeting) {
			g.Colour = Colour{Mode: "lines", Lines: []string{"#111111"}}
		}, "do not match"},
		{"one gradient stop", func(g *Greeting) { g.Colour.Stops = g.Colour.Stops[:1] }, "stops are invalid"},
		{"bad hex", func(g *Greeting) { g.Colour.Stops[0] = "#ggg000" }, "stops are invalid"},
		{"bad direction", func(g *Greeting) { g.Colour.Direction = "spiral" }, "direction"},
		{"bad mode", func(g *Greeting) { g.Colour.Mode = "rainbow" }, "colour mode"},
		{"bad message colour", func(g *Greeting) { g.MessageColour = "green" }, "message colour"},
		{"newline in context", func(g *Greeting) { g.Source.Context = "a\nb" }, "context"},
		{"javascript url", func(g *Greeting) { g.Source.URL = "javascript:alert(1)" }, "not an http link"},
		{"escape in url", func(g *Greeting) { g.Source.URL = "https://example.com/\x1b\\" }, "url"},
		{"space in url", func(g *Greeting) { g.Source.URL = "https://example.com/a b" }, "unsafe characters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := valid()
			tt.mutate(&g)
			err := g.Validate()
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && err == nil:
				t.Fatalf("expected an error containing %q", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Fatalf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestNormalise(t *testing.T) {
	g := Greeting{Art: []string{"", "  x  ", "", " y", "   "}, Message: "  hello \n"}
	g.Normalise()
	if got, want := strings.Join(g.Art, "|"), "  x|| y"; got != want {
		t.Errorf("art = %q, want %q", got, want)
	}
	if g.Message != "hello" {
		t.Errorf("message = %q", g.Message)
	}
}

func TestNewIDIsStableAndContentBased(t *testing.T) {
	a, b := valid(), valid()
	if NewID("2026-10-06", a) != NewID("2026-10-06", b) {
		t.Error("same content gave different IDs")
	}
	b.Message = "something else"
	if NewID("2026-10-06", a) == NewID("2026-10-06", b) {
		t.Error("different content gave the same ID")
	}
	if id := NewID("2026-10-06", a); !strings.HasPrefix(id, "20261006-") || len(id) != 17 {
		t.Errorf("unexpected ID format %q", id)
	}
}

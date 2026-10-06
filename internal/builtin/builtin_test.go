package builtin

import (
	"testing"

	"github.com/joeuk89/mootd/internal/greeting"
)

func TestEveryBuiltinIsAValidEvergreenGreeting(t *testing.T) {
	greetings, err := Greetings()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, g := range greetings {
		if err := g.Validate(); err != nil {
			t.Errorf("%q: %v", g.Message, err)
		}
		if g.Category != greeting.Evergreen || g.Source != nil {
			t.Errorf("%q: built-ins must be evergreen with no source", g.Message)
		}
		if seen[g.ID] {
			t.Errorf("%q: duplicate", g.Message)
		}
		seen[g.ID] = true
	}
}

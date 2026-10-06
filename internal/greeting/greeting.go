package greeting

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	ArtMinLines     = 4
	ArtMaxLines     = 12
	ArtMaxCols      = 40
	MessageMaxChars = 240
	ContextMaxChars = 70

	Evergreen = "evergreen"
)

type Greeting struct {
	ID            string   `json:"id"`
	Category      string   `json:"category"`
	ArtSubject    string   `json:"art_subject"`
	Art           []string `json:"art"`
	SpeakerCol    int      `json:"speaker_col"`
	Colour        Colour   `json:"colour"`
	Message       string   `json:"message"`
	MessageColour string   `json:"message_colour"`
	Source        *Source  `json:"source,omitempty"`
}

type Colour struct {
	Mode      string   `json:"mode"`
	Lines     []string `json:"lines,omitempty"`
	Stops     []string `json:"stops,omitempty"`
	Direction string   `json:"direction,omitempty"`
}

type Source struct {
	Context string `json:"context"`
	Outlet  string `json:"outlet"`
	URL     string `json:"url"`
}

// NewID derives the ID from the content, so importing the same greeting twice gives the same ID.
func NewID(date string, g Greeting) string {
	sum := sha256.Sum256([]byte(g.Message + "\n" + strings.Join(g.Art, "\n")))
	return strings.ReplaceAll(date, "-", "") + "-" + hex.EncodeToString(sum[:4])
}

func (g *Greeting) Normalise() {
	art := make([]string, 0, len(g.Art))
	for _, line := range g.Art {
		art = append(art, strings.TrimRight(line, " "))
	}
	for len(art) > 0 && art[0] == "" {
		art = art[1:]
	}
	for len(art) > 0 && art[len(art)-1] == "" {
		art = art[:len(art)-1]
	}
	g.Art = art
	g.Message = strings.TrimSpace(g.Message)
}

// Validate rejects anything the renderer cannot draw safely. Greetings come from a
// model fed with text from the internet, so they must never carry terminal escapes.
func (g Greeting) Validate() error {
	if len(g.Art) < ArtMinLines || len(g.Art) > ArtMaxLines {
		return fmt.Errorf("art has %d lines", len(g.Art))
	}
	for _, line := range g.Art {
		if utf8.RuneCountInString(line) > ArtMaxCols {
			return fmt.Errorf("art is wider than %d columns", ArtMaxCols)
		}
		for _, r := range line {
			if !allowedInArt(r) {
				return fmt.Errorf("art uses disallowed character %q", r)
			}
		}
	}

	if g.Message == "" || utf8.RuneCountInString(g.Message) > MessageMaxChars {
		return fmt.Errorf("message is %d characters", utf8.RuneCountInString(g.Message))
	}
	if !printable(strings.ReplaceAll(g.Message, "\n", " ")) {
		return errors.New("message contains control characters")
	}

	switch g.Colour.Mode {
	case "lines":
		if len(g.Colour.Lines) != len(g.Art) || !allHex(g.Colour.Lines) {
			return errors.New("line colours do not match the art")
		}
	case "gradient":
		if n := len(g.Colour.Stops); n < 2 || n > 3 || !allHex(g.Colour.Stops) {
			return errors.New("gradient stops are invalid")
		}
		switch g.Colour.Direction {
		case "horizontal", "vertical", "diagonal":
		default:
			return fmt.Errorf("gradient direction %q is invalid", g.Colour.Direction)
		}
	default:
		return fmt.Errorf("colour mode %q is invalid", g.Colour.Mode)
	}
	if !isHex(g.MessageColour) {
		return errors.New("message colour is invalid")
	}

	if g.Source != nil {
		return g.Source.validate()
	}
	return nil
}

func (s Source) validate() error {
	if s.Context == "" || utf8.RuneCountInString(s.Context) > ContextMaxChars || !printable(s.Context) {
		return errors.New("source context is invalid")
	}
	if s.Outlet == "" || !printable(s.Outlet) {
		return errors.New("source outlet is invalid")
	}
	u, err := url.Parse(s.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("source url is not an http link")
	}
	for i := 0; i < len(s.URL); i++ {
		if s.URL[i] <= 0x20 || s.URL[i] >= 0x7f {
			return errors.New("source url contains unsafe characters")
		}
	}
	return nil
}

// Box-drawing and block characters are one column wide in every terminal; emoji are not.
func allowedInArt(r rune) bool {
	return (r >= 0x20 && r <= 0x7e) || (r >= 0x2500 && r <= 0x259f)
}

func printable(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

func allHex(colours []string) bool {
	for _, c := range colours {
		if !isHex(c) {
			return false
		}
	}
	return true
}

func isHex(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	_, err := hex.DecodeString(s[1:])
	return err == nil
}

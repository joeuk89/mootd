package render

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"github.com/joeuk89/mootd/internal/greeting"
)

const (
	indent        = "  "
	maxBubbleText = 56
	minBubbleText = 20
	// Rows left free for the blank lines around the greeting and the shell prompt.
	spareRows = 4
)

type Options struct {
	Width      int
	Colour     ColourMode
	Hyperlinks bool
	ShowSource bool
}

// runewidth counts box-drawing characters as two columns under East Asian locales,
// but terminals draw them as one.
var width = func() *runewidth.Condition {
	c := runewidth.NewCondition()
	c.EastAsianWidth = false
	return c
}()

func Render(g greeting.Greeting, o Options) string {
	return "\n" + strings.Join(build(g, o), "\n") + "\n\n"
}

func Fits(g greeting.Greeting, o Options, rows int) bool {
	if bubbleTextWidth(o) < minBubbleText {
		return false
	}
	lines := build(g, Options{Width: o.Width, ShowSource: o.ShowSource})
	if len(lines) > rows-spareRows {
		return false
	}
	for _, line := range lines {
		if width.StringWidth(line) > o.Width {
			return false
		}
	}
	return true
}

func build(g greeting.Greeting, o Options) []string {
	lines := append(bubble(g, o), art(g, o)...)
	if o.ShowSource && g.Source != nil {
		lines = append(lines, "", sourceLine(*g.Source, o))
	}
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return lines
}

func bubbleTextWidth(o Options) int {
	return min(maxBubbleText, o.Width-len(indent)-4)
}

func bubble(g greeting.Greeting, o Options) []string {
	text := wrap(g.Message, bubbleTextWidth(o))
	inner := 0
	for _, line := range text {
		inner = max(inner, width.StringWidth(line))
	}

	out := []string{"╭" + strings.Repeat("─", inner+2) + "╮"}
	for _, line := range text {
		pad := strings.Repeat(" ", inner-width.StringWidth(line))
		out = append(out, "│ "+paint(line, g.MessageColour, o.Colour)+pad+" │")
	}

	// The bubble and the art share a left edge, so the tail sits in the speaker's column.
	tail := min(max(g.SpeakerCol, 1), inner+2)
	bottom := []rune("╰" + strings.Repeat("─", inner+2) + "╯")
	bottom[tail] = '┬'
	return append(out, string(bottom), strings.Repeat(" ", tail)+"│")
}

func wrap(text string, limit int) []string {
	var out []string
	for _, paragraph := range strings.Split(text, "\n") {
		line := ""
		for _, word := range strings.Fields(paragraph) {
			for width.StringWidth(word) > limit {
				head := width.Truncate(word, limit, "")
				if head == "" {
					break
				}
				if line != "" {
					out = append(out, line)
					line = ""
				}
				out = append(out, head)
				word = word[len(head):]
			}
			switch {
			case line == "":
				line = word
			case width.StringWidth(line)+1+width.StringWidth(word) <= limit:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
		}
		out = append(out, line)
	}
	return out
}

func art(g greeting.Greeting, o Options) []string {
	if o.Colour == Plain {
		return slices.Clone(g.Art)
	}
	if g.Colour.Mode == "gradient" {
		return gradient(g, o.Colour)
	}
	out := make([]string, len(g.Art))
	for i, line := range g.Art {
		out[i] = line
		if i < len(g.Colour.Lines) {
			out[i] = paint(line, g.Colour.Lines[i], o.Colour)
		}
	}
	return out
}

func gradient(g greeting.Greeting, mode ColourMode) []string {
	var stops []rgb
	for _, s := range g.Colour.Stops {
		if c, ok := parseHex(s); ok {
			stops = append(stops, c)
		}
	}
	if len(stops) == 0 {
		return slices.Clone(g.Art)
	}

	cols := 0
	for _, line := range g.Art {
		cols = max(cols, utf8.RuneCountInString(line))
	}

	out := make([]string, len(g.Art))
	for y, line := range g.Art {
		var b strings.Builder
		last := ""
		x := 0
		for _, ch := range line {
			if ch != ' ' {
				tx, ty := position(x, cols), position(y, len(g.Art))
				t := tx
				switch g.Colour.Direction {
				case "vertical":
					t = ty
				case "diagonal":
					t = (tx + ty) / 2
				}
				if esc := blend(stops, t).escape(mode); esc != last {
					b.WriteString(esc)
					last = esc
				}
			}
			b.WriteRune(ch)
			x++
		}
		if last != "" {
			b.WriteString(reset)
		}
		out[y] = b.String()
	}
	return out
}

func position(i, count int) float64 {
	if count < 2 {
		return 0
	}
	return float64(i) / float64(count-1)
}

func sourceLine(s greeting.Source, o Options) string {
	text := width.Truncate("re: "+s.Context+" · "+s.Outlet, o.Width-len(indent), "…")
	if o.Colour != Plain {
		text = dim + text + reset
	}
	if o.Hyperlinks {
		text = "\x1b]8;;" + s.URL + "\x1b\\" + text + "\x1b]8;;\x1b\\"
	}
	return text
}

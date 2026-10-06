package render

import (
	"fmt"
	"math"
	"strconv"
)

type ColourMode int

const (
	Plain ColourMode = iota
	Colour256
	TrueColour
)

const (
	reset = "\x1b[0m"
	dim   = "\x1b[2m"
)

type rgb struct{ r, g, b int }

func parseHex(s string) (rgb, bool) {
	if len(s) != 7 || s[0] != '#' {
		return rgb{}, false
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return rgb{}, false
	}
	return rgb{int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff)}, true
}

func (c rgb) escape(mode ColourMode) string {
	switch mode {
	case TrueColour:
		return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.r, c.g, c.b)
	case Colour256:
		return fmt.Sprintf("\x1b[38;5;%dm", c.xterm256())
	default:
		return ""
	}
}

var cubeLevels = [6]int{0, 95, 135, 175, 215, 255}

// xterm256 picks the closer of the 6x6x6 colour cube and the 24-step grey ramp.
func (c rgb) xterm256() int {
	ri, gi, bi := nearestLevel(c.r), nearestLevel(c.g), nearestLevel(c.b)
	cube := rgb{cubeLevels[ri], cubeLevels[gi], cubeLevels[bi]}

	greyIndex := min(max(((c.r+c.g+c.b)/3-8+5)/10, 0), 23)
	grey := 8 + 10*greyIndex

	if c.distance(rgb{grey, grey, grey}) < c.distance(cube) {
		return 232 + greyIndex
	}
	return 16 + 36*ri + 6*gi + bi
}

func nearestLevel(v int) int {
	best := 0
	for i, level := range cubeLevels {
		if abs(level-v) < abs(cubeLevels[best]-v) {
			best = i
		}
	}
	return best
}

func (c rgb) distance(o rgb) int {
	dr, dg, db := c.r-o.r, c.g-o.g, c.b-o.b
	return dr*dr + dg*dg + db*db
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// blend returns the colour at position t (0 to 1) along evenly spaced stops.
func blend(stops []rgb, t float64) rgb {
	if len(stops) == 1 {
		return stops[0]
	}
	scaled := min(max(t, 0), 1) * float64(len(stops)-1)
	i := min(int(scaled), len(stops)-2)
	frac := scaled - float64(i)
	mix := func(a, b int) int { return a + int(math.Round(float64(b-a)*frac)) }
	from, to := stops[i], stops[i+1]
	return rgb{mix(from.r, to.r), mix(from.g, to.g), mix(from.b, to.b)}
}

func paint(text, hexColour string, mode ColourMode) string {
	c, ok := parseHex(hexColour)
	if !ok || mode == Plain {
		return text
	}
	return c.escape(mode) + text + reset
}

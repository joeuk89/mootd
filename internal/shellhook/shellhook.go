// Package shellhook edits a shell startup file to run mootd in each new terminal.
package shellhook

import (
	"fmt"
	"strings"
)

const (
	startMarker = "# >>> mootd >>>"
	endMarker   = "# <<< mootd <<<"
	// Powerlevel10k's instant prompt warns about anything printed after its block
	// in .zshrc has run, so the greeting has to go above it.
	instantPrompt = "p10k-instant-prompt"
)

// Block returns the lines that print a greeting in interactive shells. It names
// the binary by full path, because a startup file can run before PATH is complete.
func Block(binary string) string {
	return fmt.Sprintf("%s\n[[ $- == *i* ]] && [[ -x %q ]] && %q\n%s\n", startMarker, binary, binary, endMarker)
}

func Installed(content string) bool {
	return strings.Contains(content, startMarker)
}

// Insert adds the block to a startup file's content: above the Powerlevel10k instant
// prompt block when there is one, otherwise at the end. The second result reports
// whether it went above the instant prompt.
func Insert(content, block string) (string, bool) {
	lines := strings.SplitAfter(content, "\n")
	for i, line := range lines {
		if !strings.Contains(line, instantPrompt) {
			continue
		}
		for i > 0 && strings.HasPrefix(lines[i-1], "#") {
			i--
		}
		return strings.Join(lines[:i], "") + block + "\n" + strings.Join(lines[i:], ""), true
	}

	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if content != "" {
		content += "\n"
	}
	return content + block, false
}

// Remove takes the block back out, along with the blank line Insert put beside it.
func Remove(content string) (string, bool) {
	start := strings.Index(content, startMarker)
	if start < 0 {
		return content, false
	}
	end := strings.Index(content[start:], endMarker)
	if end < 0 {
		return content, false
	}
	end += start + len(endMarker)
	before, after := content[:start], strings.TrimPrefix(content[end:], "\n")

	switch {
	case strings.HasPrefix(after, "\n"):
		after = after[1:]
	case strings.HasSuffix(before, "\n\n"):
		before = before[:len(before)-1]
	}
	return before + after, true
}

package config

import (
	"fmt"
	"strings"
)

// In the template a line starting "# " is prose and a line starting "#" with no
// space is a setting at its default, ready to be uncommented.
const templateHead = `# mootd settings.
#
# Each setting below is commented out and shows its default.
# To change one, remove the leading "#" and edit the value.

# The Claude model that writes and scores the greetings.
#model = %q

# How hard the model thinks: "low", "medium", "high", or "" for the model's default.
# Lower effort is faster and uses less of your plan, but draws plainer art.
#effort = %q

# Have a second call score the greetings and keep only the best.
#cull = %t

# Days a topical greeting stays in rotation.
#expiry_days = %d

# Show the dim "re: ..." line that links to the news story.
#show_source = %t

# Terminal programs that should not show greetings, matched against
# $TERM_PROGRAM and $TERMINAL_EMULATOR. For example: ["vscode", "JetBrains-JediTerm"]
#skip_terminals = []

# Full path to the claude command. Leave empty to find it automatically.
#claude_bin = ""

# The Claude config directory to use, if you have more than one account.
#claude_config_dir = ""

# Steer the humour in your own words, for example style = "dry, deadpan"
# and interests = "Formula 1, synths".
#style = ""
#interests = ""

# How many greetings to keep per category in each daily batch. "evergreen"
# greetings are timeless jokes with no headline. A [mix] table replaces the
# whole default mix.
#[mix]
`

// Template returns a config file with every setting commented out at its default.
func Template() string {
	d := Default()
	var b strings.Builder
	fmt.Fprintf(&b, templateHead, d.Model, d.Effort, d.Cull, d.ExpiryDays, d.ShowSource)
	for _, category := range d.Categories() {
		fmt.Fprintf(&b, "#%s = %d\n", category, d.Mix[category])
	}
	b.WriteString("\n# News feeds, RSS or Atom. Any [[feeds]] entry replaces the whole default list.\n")
	for _, f := range d.Feeds {
		fmt.Fprintf(&b, "#[[feeds]]\n#category = %q\n#outlet = %q\n#url = %q\n", f.Category, f.Outlet, f.URL)
	}
	return b.String()
}

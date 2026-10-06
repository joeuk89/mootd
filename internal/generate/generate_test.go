package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/joeuk89/mootd/internal/config"
	"github.com/joeuk89/mootd/internal/greeting"
	"github.com/joeuk89/mootd/internal/store"
)

var now = time.Date(2026, 10, 6, 9, 0, 0, 0, time.Local)

// fakeClaude stands in for the claude command. It records its arguments, prompt and
// environment, then prints a canned reply chosen by which schema it was given.
const fakeClaude = `#!/bin/sh
kind=generate
case "$*" in *'"scores"'*) kind=cull ;; esac
cat > "$FAKE_DIR/$kind.prompt"
printf '%s\n' "$@" > "$FAKE_DIR/$kind.args"
echo "$MOOTD_SKIP|$CLAUDE_CONFIG_DIR|$PWD" > "$FAKE_DIR/$kind.env"
if [ -f "$FAKE_DIR/$kind.fail" ]; then echo "boom" >&2; exit 1; fi
cat "$FAKE_DIR/$kind.reply"
`

type harness struct {
	t    *testing.T
	dir  string
	gen  Generator
	logs *strings.Builder
}

func newHarness(t *testing.T, mix map[string]int) *harness {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FAKE_DIR", dir)
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte(fakeClaude), 0o755); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/news" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `<rss><channel>
			<item><title>Council bans gravity</title><link>https://example.com/gravity</link></item>
			<item><title>Robot wins bake-off</title><link>https://example.com/bake</link></item>
		</channel></rss>`)
	}))
	t.Cleanup(srv.Close)

	st, err := store.OpenAt(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	logs := &strings.Builder{}
	cfg := config.Default()
	cfg.ClaudeBin = bin
	cfg.ClaudeConfigDir = "/somewhere/claude"
	cfg.Mix = mix
	cfg.Feeds = []config.Feed{{Category: "news", Outlet: "The Example", URL: srv.URL + "/news"}}
	return &harness{t: t, dir: dir, logs: logs, gen: Generator{
		Config: cfg,
		Store:  st,
		Log:    log.New(io.MultiWriter(logs, testWriter{t}), "", 0),
		HTTP:   srv.Client(),
	}}
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimSpace(string(p)))
	return len(p), nil
}

func (h *harness) reply(kind string, payload any) {
	h.t.Helper()
	data, err := json.Marshal(map[string]any{"is_error": false, "structured_output": payload})
	if err != nil {
		h.t.Fatal(err)
	}
	h.file(kind+".reply", string(data))
}

func (h *harness) file(name, body string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.dir, name), []byte(body), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) read(name string) string {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.dir, name))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(data)
}

func (h *harness) pooled() []string {
	h.t.Helper()
	var messages []string
	err := h.gen.Store.Update(func(p *store.Pool) error {
		for _, e := range p.Entries {
			messages = append(messages, e.Greeting.Message)
		}
		return nil
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return messages
}

func candidateJSON(category, message string, headline any) map[string]any {
	var context any
	if headline != nil {
		context = "Something happened"
	}
	return map[string]any{
		"category":       category,
		"art_subject":    "small cat",
		"art":            []string{" /\\_/\\", "( o.o )", " > ^ <", " /   \\"},
		"speaker_col":    3,
		"colour":         map[string]any{"mode": "gradient", "stops": []string{"#ff8800", "#3366ff"}, "direction": "vertical"},
		"message":        message,
		"message_colour": "#44aa66",
		"headline_id":    headline,
		"context":        context,
	}
}

func sixCandidates() map[string]any {
	emoji := candidateJSON("evergreen", "bad art", nil)
	emoji["art"] = []string{"🐄", "x", "x", "x"}
	return map[string]any{"greetings": []any{
		candidateJSON("news", "news one", "H1"),
		candidateJSON("news", "news two", "H2"),
		candidateJSON("evergreen", "evergreen one", nil),
		candidateJSON("evergreen", "evergreen two", nil),
		emoji,
		candidateJSON("news", "made-up headline", "H99"),
		candidateJSON("sport", "wrong category", "H1"),
	}}
}

func TestRunKeepsTheBestOfEachCategory(t *testing.T) {
	h := newHarness(t, map[string]int{"news": 1, "evergreen": 1})
	h.reply("generate", sixCandidates())
	h.reply("cull", map[string]any{"scores": []any{
		map[string]any{"id": "g01", "humour": 5, "art": 5, "reason": "fine"},
		map[string]any{"id": "g02", "humour": 8, "art": 8, "reason": "great"},
		map[string]any{"id": "g03", "humour": 2, "art": 9, "reason": "tasteless"},
		map[string]any{"id": "g04", "humour": 6, "art": 6, "reason": "good"},
	}})

	added, err := h.gen.Run(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.pooled(); added != 2 || !slices.Equal(got, []string{"news two", "evergreen two"}) {
		t.Fatalf("added %d, pool = %v", added, got)
	}

	err = h.gen.Store.Update(func(p *store.Pool) error {
		news, evergreen := p.Entries[0], p.Entries[1]
		src := news.Greeting.Source
		if src == nil || src.URL != "https://example.com/bake" || src.Outlet != "The Example" || src.Context != "Something happened" {
			t.Errorf("news source = %+v", src)
		}
		if news.Expires != "2026-10-09" || news.Added != "2026-10-06" {
			t.Errorf("news entry dates: added %s, expires %s", news.Added, news.Expires)
		}
		if evergreen.Greeting.Source != nil || evergreen.Expires != "" {
			t.Errorf("evergreen entry = %+v", evergreen)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	prompt := h.read("generate.prompt")
	for _, want := range []string{
		"Today is Tuesday 6 October 2026.",
		"[H1] (The Example) Council bans gravity",
		"[H2] (The Example) Robot wins bake-off",
		"- 2 news\n- 2 evergreen",
		"untrusted text",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("generate prompt is missing %q", want)
		}
	}
	if strings.Contains(prompt, "Already told") {
		t.Error("first run should have nothing already told")
	}
	if cull := h.read("cull.prompt"); !strings.Contains(cull, "=== id: g04 | category: evergreen") || strings.Contains(cull, "g05") {
		t.Errorf("cull prompt should list exactly the four valid greetings:\n%s", cull)
	}

	for _, want := range []string{"rejected greeting 5", "unknown headline id \"H99\"", "unexpected category \"sport\""} {
		if !strings.Contains(h.logs.String(), want) {
			t.Errorf("log is missing %q", want)
		}
	}
}

func TestRunLocksDownTheClaudeCall(t *testing.T) {
	h := newHarness(t, map[string]int{"news": 1})
	h.gen.Config.Cull = false
	h.gen.Config.Effort = "low"
	h.gen.Config.Style = "deadpan"
	h.reply("generate", sixCandidates())

	if _, err := h.gen.Run(context.Background(), now); err != nil {
		t.Fatal(err)
	}

	args := strings.Split(strings.TrimSuffix(h.read("generate.args"), "\n"), "\n")
	follows := func(flag, value string) bool {
		i := slices.Index(args, flag)
		return i >= 0 && i+1 < len(args) && args[i+1] == value
	}
	if !follows("--tools", "") || !follows("--setting-sources", "") || !follows("--settings", `{"disableAllHooks":true}`) {
		t.Errorf("lock-down flags are wrong: %q", args)
	}
	if !follows("--model", "claude-opus-5-5") || !follows("--effort", "low") {
		t.Errorf("model flags are wrong: %q", args)
	}
	for _, flag := range []string{"-p", "--strict-mcp-config", "--disable-slash-commands", "--no-session-persistence"} {
		if !slices.Contains(args, flag) {
			t.Errorf("missing flag %s", flag)
		}
	}
	if !strings.Contains(strings.Join(args, "\n"), "The style of humour they like: deadpan") {
		t.Error("system prompt is missing the style setting")
	}

	env := strings.Split(strings.TrimSpace(h.read("generate.env")), "|")
	wd, _ := os.Getwd()
	if env[0] != "1" || env[1] != "/somewhere/claude" || env[2] == wd {
		t.Errorf("claude ran with MOOTD_SKIP=%q CLAUDE_CONFIG_DIR=%q in %q", env[0], env[1], env[2])
	}
	if _, err := os.Stat(filepath.Join(h.dir, "cull.args")); err == nil {
		t.Error("cull call ran with cull switched off")
	}
	if got := h.pooled(); !slices.Equal(got, []string{"news one"}) {
		t.Errorf("without the cull pass the first valid greeting per category is kept, got %v", got)
	}
}

func TestRunTellsTheModelWhatItAlreadyTold(t *testing.T) {
	h := newHarness(t, map[string]int{"news": 1})
	h.gen.Config.Cull = false
	h.reply("generate", sixCandidates())
	if _, err := h.gen.Run(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := h.gen.Run(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if prompt := h.read("generate.prompt"); !strings.Contains(prompt, "# Already told") || !strings.Contains(prompt, "- news one") {
		t.Errorf("second prompt should list the first run's message:\n%s", prompt)
	}
}

func TestRunKeepsUnscoredGreetingsWhenTheCullFails(t *testing.T) {
	h := newHarness(t, map[string]int{"news": 1, "evergreen": 1})
	h.reply("generate", sixCandidates())
	h.file("cull.fail", "")

	if _, err := h.gen.Run(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if got := h.pooled(); !slices.Equal(got, []string{"news one", "evergreen one"}) {
		t.Errorf("pool = %v", got)
	}
}

func TestRunReportsAClaudeFailure(t *testing.T) {
	h := newHarness(t, map[string]int{"news": 1})
	h.file("generate.fail", "")

	_, err := h.gen.Run(context.Background(), now)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want one mentioning the claude output", err)
	}
	if got := h.pooled(); len(got) != 0 {
		t.Errorf("pool = %v", got)
	}
}

func TestRunWithoutHeadlines(t *testing.T) {
	h := newHarness(t, map[string]int{"news": 1})
	h.gen.Config.Feeds[0].URL += "-missing"
	if _, err := h.gen.Run(context.Background(), now); err == nil || !strings.Contains(err.Error(), "no headlines") {
		t.Fatalf("error = %v", err)
	}

	h = newHarness(t, map[string]int{"news": 1, "evergreen": 1})
	h.gen.Config.Feeds[0].URL += "-missing"
	h.gen.Config.Cull = false
	h.reply("generate", map[string]any{"greetings": []any{candidateJSON("evergreen", "still here", nil)}})
	if _, err := h.gen.Run(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if got := h.pooled(); !slices.Equal(got, []string{"still here"}) {
		t.Errorf("pool = %v", got)
	}
	if prompt := h.read("generate.prompt"); strings.Contains(prompt, "## news") || !strings.Contains(prompt, "\n\n- 1 evergreen\n\n") {
		t.Errorf("prompt should ask for evergreen greetings only:\n%s", prompt)
	}
}

func TestRunPassesFeedbackToTheModel(t *testing.T) {
	h := newHarness(t, map[string]int{"news": 1})
	h.gen.Config.Cull = false
	h.reply("generate", sixCandidates())

	if _, err := h.gen.Run(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.read("generate.args"), "The reader's taste") {
		t.Error("the taste section should be absent when there is no feedback")
	}

	feedback := func(verdict, subject, message, reason string) {
		t.Helper()
		g := greeting.Greeting{ID: message, ArtSubject: subject, Message: message}
		if err := h.gen.Store.AddFeedback(store.Feedback{Verdict: verdict, Reason: reason, Greeting: g}); err != nil {
			t.Fatal(err)
		}
	}
	feedback(store.Keep, "smug cat", "line one\nline two", "")
	feedback(store.Nope, "sad robot", "an AI joke", "too many AI jokes")

	if _, err := h.gen.Run(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	system := h.read("generate.args")
	for _, want := range []string{
		"# The reader's taste",
		"- (art: smug cat) line one / line two\n",
		"- (art: sad robot) an AI joke [their reason: too many AI jokes]\n",
		"\n# Output\n",
	} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt is missing %q", want)
		}
	}
}

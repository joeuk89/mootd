package feeds

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/joeuk89/mootd/internal/config"
)

const rss = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">
<channel>
  <title>Example</title>
  <link>https://example.com/</link>
  <atom:link href="https://example.com/rss" rel="self"/>
  <item>
    <title>Cats &amp;amp; dogs sign treaty</title>
    <link>https://example.com/treaty</link>
    <atom:link href="https://example.com/ignored"/>
    <description>&lt;p&gt;A &lt;b&gt;historic&lt;/b&gt;   day.&lt;/p&gt;</description>
  </item>
  <item>
    <title>No link here</title>
  </item>
  <item>
    <title>Bad` + "\x1b" + `[31m escape</title>
    <link>javascript:alert(1)</link>
  </item>
  <item>
    <title>Second story</title>
    <link>https://example.com/second</link>
  </item>
</channel>
</rss>`

const atom = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Example Atom</title>
  <entry>
    <title type="html">Atom &amp;lt;em&amp;gt;story&amp;lt;/em&amp;gt;</title>
    <link rel="replies" href="https://example.com/comments"/>
    <link rel="alternate" href="https://example.com/atom-story"/>
    <summary>Summary text</summary>
  </entry>
</feed>`

func TestParseRSS(t *testing.T) {
	// A raw escape byte is not valid XML, so the fixture swaps it for a character reference first.
	body := strings.ReplaceAll(rss, "\x1b", "&#x9b;")
	got, err := parse([]byte(body), config.Feed{Category: "uk", Outlet: "Example"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d headlines, want 2: %+v", len(got), got)
	}
	want := Headline{
		Category: "uk",
		Outlet:   "Example",
		Title:    "Cats & dogs sign treaty",
		Summary:  "A historic day.",
		URL:      "https://example.com/treaty",
	}
	if got[0] != want {
		t.Errorf("first headline = %+v, want %+v", got[0], want)
	}
	if got[1].URL != "https://example.com/second" {
		t.Errorf("second headline = %+v", got[1])
	}
}

func TestParseAtom(t *testing.T) {
	got, err := parse([]byte(atom), config.Feed{Category: "programming", Outlet: "Atom"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Atom story" || got[0].URL != "https://example.com/atom-story" || got[0].Summary != "Summary text" {
		t.Errorf("got %+v", got)
	}
}

func TestCleanStripsControlCharactersAndTruncates(t *testing.T) {
	if got := clean("a\x1b[31mb‮c\n\td", 100); got != "a [31mb c d" {
		t.Errorf("got %q", got)
	}
	if got := clean(strings.Repeat("word ", 100), 12); got != "word word wo…" {
		t.Errorf("got %q", got)
	}
}

func feedOf(n int, prefix string) string {
	var b strings.Builder
	b.WriteString(`<rss><channel>`)
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, `<item><title>%s %d</title><link>https://example.com/%s/%d</link></item>`, prefix, i, prefix, i)
	}
	b.WriteString(`</channel></rss>`)
	return b.String()
}

func TestFetchInterleavesFeedsAndSkipsFailures(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, feedOf(5, "a")) })
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, feedOf(1, "b")) })
	mux.HandleFunc("/c", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, feedOf(2, "c")) })
	mux.HandleFunc("/broken", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusInternalServerError) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var logged []string
	logf := func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }
	got := Fetch(context.Background(), srv.Client(), []config.Feed{
		{Category: "uk", Outlet: "A", URL: srv.URL + "/a"},
		{Category: "world", Outlet: "C", URL: srv.URL + "/c"},
		{Category: "uk", Outlet: "B", URL: srv.URL + "/b"},
		{Category: "uk", Outlet: "Broken", URL: srv.URL + "/broken"},
	}, 4, logf)

	var summary []string
	for _, h := range got {
		summary = append(summary, h.ID+":"+h.Category+":"+h.Title)
	}
	want := "H1:uk:a 1, H2:uk:b 1, H3:uk:a 2, H4:uk:a 3, H5:world:c 1, H6:world:c 2"
	if s := strings.Join(summary, ", "); s != want {
		t.Errorf("got  %s\nwant %s", s, want)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "Broken") {
		t.Errorf("expected one log line about the broken feed, got %v", logged)
	}
}

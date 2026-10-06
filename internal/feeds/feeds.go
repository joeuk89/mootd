package feeds

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/joeuk89/mootd/internal/config"
)

const (
	userAgent       = "mootd (+https://github.com/joeuk89/mootd)"
	maxBodyBytes    = 5 << 20
	maxTitleRunes   = 200
	maxSummaryRunes = 200
)

type Headline struct {
	ID       string
	Category string
	Outlet   string
	Title    string
	Summary  string
	URL      string
}

// document covers RSS 2.0 (channel>item), RSS 1.0 (item at the root) and Atom (entry).
type document struct {
	RSSItems []rssItem   `xml:"channel>item"`
	RDFItems []rssItem   `xml:"item"`
	Entries  []atomEntry `xml:"entry"`
}

type rssItem struct {
	Title string `xml:"title"`
	// Items can also hold an empty <atom:link>, so collect every link and use the first with text.
	Links       []string `xml:"link"`
	Description string   `xml:"description"`
}

type atomEntry struct {
	Title string `xml:"title"`
	Links []struct {
		Rel  string `xml:"rel,attr"`
		Href string `xml:"href,attr"`
	} `xml:"link"`
	Summary string `xml:"summary"`
}

// Fetch downloads every feed at once and returns up to perCategory headlines for each
// category. A feed that fails is logged and skipped.
func Fetch(ctx context.Context, client *http.Client, feeds []config.Feed, perCategory int, logf func(string, ...any)) []Headline {
	results := make([][]Headline, len(feeds))
	var wg sync.WaitGroup
	for i, feed := range feeds {
		wg.Go(func() {
			items, err := fetchOne(ctx, client, feed)
			if err != nil {
				logf("feed failed: %s (%s): %v", feed.Outlet, feed.URL, err)
				return
			}
			results[i] = items
		})
	}
	wg.Wait()

	var categories []string
	byCategory := map[string][][]Headline{}
	for i, feed := range feeds {
		if _, seen := byCategory[feed.Category]; !seen {
			categories = append(categories, feed.Category)
		}
		byCategory[feed.Category] = append(byCategory[feed.Category], results[i])
	}

	var out []Headline
	for _, category := range categories {
		// Take turns across feeds so one busy outlet does not crowd out the others.
		picked := 0
		for rank := 0; picked < perCategory; rank++ {
			found := false
			for _, items := range byCategory[category] {
				if rank < len(items) && picked < perCategory {
					h := items[rank]
					h.ID = fmt.Sprintf("H%d", len(out)+1)
					out = append(out, h)
					picked++
					found = true
				}
			}
			if !found {
				break
			}
		}
	}
	return out
}

func fetchOne(ctx context.Context, client *http.Client, feed config.Feed) ([]Headline, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, err
	}
	return parse(body, feed)
}

func parse(body []byte, feed config.Feed) ([]Headline, error) {
	var doc document
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, err
	}

	var out []Headline
	add := func(title, link, summary string) {
		title, link = clean(title, maxTitleRunes), strings.TrimSpace(link)
		if title == "" || !isHTTP(link) {
			return
		}
		out = append(out, Headline{
			Category: feed.Category,
			Outlet:   feed.Outlet,
			Title:    title,
			Summary:  clean(summary, maxSummaryRunes),
			URL:      link,
		})
	}

	for _, it := range append(doc.RSSItems, doc.RDFItems...) {
		link := ""
		for _, l := range it.Links {
			if strings.TrimSpace(l) != "" {
				link = l
				break
			}
		}
		add(it.Title, link, it.Description)
	}
	for _, e := range doc.Entries {
		link := ""
		for _, l := range e.Links {
			if link == "" || l.Rel == "alternate" {
				link = l.Href
			}
		}
		add(e.Title, link, e.Summary)
	}
	return out, nil
}

var (
	tags       = regexp.MustCompile(`<[^>]*>`)
	whitespace = regexp.MustCompile(`\s+`)
)

// clean turns feed text, which may hold escaped HTML, into one line of plain text
// with no control characters.
func clean(s string, maxRunes int) string {
	s = html.UnescapeString(tags.ReplaceAllString(html.UnescapeString(s), " "))
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, s)
	s = strings.TrimSpace(whitespace.ReplaceAllString(s, " "))
	if runes := []rune(s); len(runes) > maxRunes {
		s = strings.TrimSpace(string(runes[:maxRunes])) + "…"
	}
	return s
}

func isHTTP(link string) bool {
	return strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "http://")
}

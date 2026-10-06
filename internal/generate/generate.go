package generate

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/joeuk89/mootd/internal/config"
	"github.com/joeuk89/mootd/internal/feeds"
	"github.com/joeuk89/mootd/internal/greeting"
	"github.com/joeuk89/mootd/internal/store"
)

const (
	headlinesPerCategory = 15
	alreadyToldCount     = 40
	feedbackCount        = 15
	// With the cull pass on, ask for this many greetings per one kept.
	overAsk = 1.5
	// The judge scores 1-2 for a confusing or tasteless joke, or misaligned art.
	// Those are dropped even if that leaves a category short.
	minScore = 3

	feedTimeout     = 30 * time.Second
	generateTimeout = 15 * time.Minute
	cullTimeout     = 5 * time.Minute
)

type Generator struct {
	Config config.Config
	Store  *store.Store
	Log    *log.Logger
	HTTP   *http.Client
}

type candidate struct {
	greeting.Greeting
	HeadlineID *string `json:"headline_id"`
	Context    *string `json:"context"`
}

type score struct {
	ID     string `json:"id"`
	Humour int    `json:"humour"`
	Art    int    `json:"art"`
	Reason string `json:"reason"`
}

// Run makes one batch and adds it to the pool. It returns how many greetings it added.
func (g *Generator) Run(ctx context.Context, now time.Time) (int, error) {
	cfg := g.Config
	bin, err := FindClaude(cfg.ClaudeBin)
	if err != nil {
		return 0, err
	}
	cl := claude{bin: bin, configDir: cfg.ClaudeConfigDir, model: cfg.Model, effort: cfg.Effort}

	feedCtx, cancel := context.WithTimeout(ctx, feedTimeout)
	headlines := feeds.Fetch(feedCtx, g.HTTP, cfg.Feeds, headlinesPerCategory, g.Log.Printf)
	cancel()

	fetched := map[string]int{}
	for _, h := range headlines {
		fetched[h.Category]++
	}
	var categories []string
	ask := map[string]int{}
	for _, category := range cfg.Categories() {
		if category != greeting.Evergreen && fetched[category] == 0 {
			g.Log.Printf("skipping %s: no headlines fetched", category)
			continue
		}
		categories = append(categories, category)
		ask[category] = cfg.Mix[category]
		if cfg.Cull {
			ask[category] = int(float64(cfg.Mix[category])*overAsk + 0.5)
		}
	}
	if len(categories) == 0 {
		return 0, errors.New("no headlines could be fetched")
	}
	g.Log.Printf("fetched %d headlines; asking %s for %v", len(headlines), cfg.Model, ask)

	told, err := g.Store.RecentMessages(alreadyToldCount)
	if err != nil {
		return 0, err
	}
	kept, noped, err := g.Store.RecentFeedback(feedbackCount)
	if err != nil {
		return 0, err
	}
	system, err := generateSystem(cfg, kept, noped)
	if err != nil {
		return 0, err
	}

	var reply struct {
		Greetings []candidate `json:"greetings"`
	}
	genCtx, cancel := context.WithTimeout(ctx, generateTimeout)
	err = cl.call(genCtx, system, generatePrompt(now, categories, headlines, ask, told), greetingSchema(categories), &reply)
	cancel()
	if err != nil {
		return 0, err
	}

	valid := g.validate(reply.Greetings, headlines, ask, now)
	if len(valid) == 0 {
		return 0, fmt.Errorf("none of the %d greetings passed validation", len(reply.Greetings))
	}

	chosen := firstPerCategory(valid, cfg.Mix)
	if cfg.Cull {
		var scored struct {
			Scores []score `json:"scores"`
		}
		cullCtx, cancel := context.WithTimeout(ctx, cullTimeout)
		err := cl.call(cullCtx, cullSystem, cullPrompt(valid), scoreSchema(), &scored)
		cancel()
		if err != nil {
			g.Log.Printf("cull pass failed, keeping unscored greetings: %v", err)
		} else {
			chosen = g.best(valid, scored.Scores, cfg.Mix)
		}
	}

	added, err := g.Store.AddBatch(chosen, now, cfg.ExpiryDays)
	if err != nil {
		return 0, err
	}
	g.Log.Printf("added %d greetings", added)
	return added, nil
}

func (g *Generator) validate(candidates []candidate, headlines []feeds.Headline, ask map[string]int, now time.Time) []greeting.Greeting {
	byID := map[string]feeds.Headline{}
	for _, h := range headlines {
		byID[h.ID] = h
	}

	var valid []greeting.Greeting
	for i, c := range candidates {
		gr := c.Greeting
		gr.Normalise()
		err := func() error {
			if ask[gr.Category] == 0 {
				return fmt.Errorf("unexpected category %q", gr.Category)
			}
			if gr.Category != greeting.Evergreen {
				if c.HeadlineID == nil || c.Context == nil {
					return errors.New("topical greeting has no headline")
				}
				h, ok := byID[*c.HeadlineID]
				if !ok {
					return fmt.Errorf("unknown headline id %q", *c.HeadlineID)
				}
				gr.Source = &greeting.Source{Context: strings.TrimSpace(*c.Context), Outlet: h.Outlet, URL: h.URL}
			}
			return gr.Validate()
		}()
		if err != nil {
			g.Log.Printf("rejected greeting %d (%s): %v", i+1, gr.ArtSubject, err)
			continue
		}
		gr.ID = greeting.NewID(now.Format("2006-01-02"), gr)
		valid = append(valid, gr)
	}
	return valid
}

// best keeps the highest-scoring greetings in each category, up to the mix.
func (g *Generator) best(candidates []greeting.Greeting, scores []score, mix map[string]int) []greeting.Greeting {
	byID := map[string]score{}
	for _, s := range scores {
		byID[s.ID] = s
	}

	type ranked struct {
		greeting greeting.Greeting
		score    score
	}
	logVerdict := func(verdict string, c greeting.Greeting, s score) {
		g.Log.Printf("%s %s (%s): humour %d, art %d: %s", verdict, c.ID, c.ArtSubject, s.Humour, s.Art, s.Reason)
	}

	var pool []ranked
	for i, c := range candidates {
		s := byID[candidateID(i)]
		if s.Humour < minScore || s.Art < minScore {
			logVerdict("cut", c, s)
			continue
		}
		pool = append(pool, ranked{c, s})
	}
	slices.SortStableFunc(pool, func(a, b ranked) int {
		return cmp.Compare(b.score.Humour+b.score.Art, a.score.Humour+a.score.Art)
	})

	var kept []greeting.Greeting
	taken := map[string]int{}
	for _, r := range pool {
		category := r.greeting.Category
		if taken[category] >= mix[category] {
			logVerdict("cut", r.greeting, r.score)
			continue
		}
		taken[category]++
		kept = append(kept, r.greeting)
		logVerdict("kept", r.greeting, r.score)
	}
	return kept
}

func firstPerCategory(candidates []greeting.Greeting, mix map[string]int) []greeting.Greeting {
	var kept []greeting.Greeting
	taken := map[string]int{}
	for _, c := range candidates {
		if taken[c.Category] < mix[c.Category] {
			taken[c.Category]++
			kept = append(kept, c)
		}
	}
	return kept
}

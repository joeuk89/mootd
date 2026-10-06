// Package builtin holds the greetings that ship with the program, so there is
// something to show before the first generation and when generation fails.
package builtin

import (
	_ "embed"
	"encoding/json"

	"github.com/joeuk89/mootd/internal/greeting"
)

// Version must go up whenever greetings.json changes, so that existing installs
// add the new greetings to their pool.
const Version = 1

//go:embed greetings.json
var data []byte

func Greetings() ([]greeting.Greeting, error) {
	var greetings []greeting.Greeting
	if err := json.Unmarshal(data, &greetings); err != nil {
		return nil, err
	}
	for i := range greetings {
		greetings[i].ID = greeting.NewID("builtin", greetings[i])
	}
	return greetings, nil
}

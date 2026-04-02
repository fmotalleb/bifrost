package proxy

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"sync"
	"time"

	"github.com/fmotalleb/bifrost/config"
)

type weightedChoice struct {
	name       string
	cumulative int
}

// Selector chooses interface names based on configured weights.
type Selector struct {
	choices []weightedChoice
	total   int
	rng     *rand.Rand
	mu      sync.Mutex
}

// NewSelector builds a weighted selector from interface config.
func NewSelector(ifaces map[string]config.Iface) (*Selector, error) {
	if len(ifaces) == 0 {
		return nil, fmt.Errorf("no interfaces configured")
	}

	names := make([]string, 0, len(ifaces))
	for name := range ifaces {
		names = append(names, name)
	}
	sort.Strings(names)

	choices := make([]weightedChoice, 0, len(names))
	total := 0
	for _, name := range names {
		weight := ifaces[name].Weight
		if weight <= 0 {
			return nil, fmt.Errorf("invalid weight for interface %q: %d", name, weight)
		}

		total += weight
		choices = append(choices, weightedChoice{name: name, cumulative: total})
	}

	seed := uint64(time.Now().UnixNano())

	return &Selector{
		choices: choices,
		total:   total,
		rng:     rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)),
	}, nil
}

// Pick returns one interface name based on weight distribution.
func (s *Selector) Pick() (string, error) {
	if s == nil || s.total <= 0 {
		return "", fmt.Errorf("selector is not initialized")
	}

	s.mu.Lock()
	r := s.rng.IntN(s.total) + 1
	s.mu.Unlock()

	for _, choice := range s.choices {
		if r <= choice.cumulative {
			return choice.name, nil
		}
	}

	return "", fmt.Errorf("selector failed to choose an interface")
}

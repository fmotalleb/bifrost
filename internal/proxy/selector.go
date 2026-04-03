package proxy

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/fmotalleb/bifrost/config"
)

type weightedChoice struct {
	name   string
	weight int
}

// Selector chooses interface names based on configured weights.
type Selector struct {
	choices []weightedChoice
	active  map[string]int
	nextTie int
	mu      sync.Mutex
}

// NewSelector builds a weighted selector from interface config.
func NewSelector(ifaces map[string]config.Iface) (*Selector, error) {
	if len(ifaces) == 0 {
		return nil, errors.New("no interfaces configured")
	}

	names := make([]string, 0, len(ifaces))
	for name := range ifaces {
		names = append(names, name)
	}
	sort.Strings(names)

	choices := make([]weightedChoice, 0, len(names))
	for _, name := range names {
		weight := ifaces[name].Weight
		if weight <= 0 {
			return nil, fmt.Errorf("invalid weight for interface %q: %d", name, weight)
		}

		choices = append(choices, weightedChoice{name: name, weight: weight})
	}

	return &Selector{
		choices: choices,
		active:  make(map[string]int, len(choices)),
	}, nil
}

// Pick acquires one interface name using weighted least-active selection.
// Interfaces with lower active/weight ratio are preferred.
func (s *Selector) Pick() (string, error) {
	if s == nil || len(s.choices) == 0 {
		return "", errors.New("selector is not initialized")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	selected := s.choices[0]
	equivalent := make([]weightedChoice, 0, len(s.choices))
	equivalent = append(equivalent, selected)
	for _, choice := range s.choices[1:] {
		cmp := compareLoadRatio(s.active[choice.name], choice.weight, s.active[selected.name], selected.weight)
		switch {
		case cmp < 0:
			selected = choice
			equivalent = equivalent[:0]
			equivalent = append(equivalent, choice)
		case cmp == 0:
			equivalent = append(equivalent, choice)
		}
	}

	if len(equivalent) > 1 {
		selected = equivalent[s.nextTie%len(equivalent)]
		s.nextTie++
	}

	s.active[selected.name]++
	return selected.name, nil
}

// Release marks one previously acquired interface slot as complete.
func (s *Selector) Release(name string) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	active := s.active[name]
	if active <= 1 {
		delete(s.active, name)
		return
	}
	s.active[name] = active - 1
}

// compareLoadRatio compares active/weight for A and B without floats.
func compareLoadRatio(activeA, weightA, activeB, weightB int) int {
	left := activeA * weightB
	right := activeB * weightA
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

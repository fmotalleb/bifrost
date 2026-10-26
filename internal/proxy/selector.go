package proxy

import (
	"errors"
	"fmt"
	"math/bits"
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
	return s.PickExcluding(nil)
}

// PickExcluding acquires the least-loaded interface not present in excluded.
func (s *Selector) PickExcluding(excluded map[string]struct{}) (string, error) {
	if s == nil || len(s.choices) == 0 {
		return "", errors.New("selector is not initialized")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var selected weightedChoice
	found := false
	for _, choice := range s.choices {
		if _, skip := excluded[choice.name]; skip {
			continue
		}
		if !found {
			selected = choice
			found = true
			continue
		}
		cmp := compareLoadRatio(s.active[choice.name], choice.weight, s.active[selected.name], selected.weight)
		if cmp < 0 {
			selected = choice
		}
	}
	if !found {
		return "", errors.New("no eligible interfaces remain")
	}

	equivalentCount := 0
	for _, choice := range s.choices {
		if _, skip := excluded[choice.name]; skip {
			continue
		}
		if compareLoadRatio(s.active[choice.name], choice.weight, s.active[selected.name], selected.weight) == 0 {
			equivalentCount++
		}
	}

	if equivalentCount > 1 {
		tieOffset := s.nextTie % equivalentCount
		target := tieOffset
		for _, choice := range s.choices {
			if _, skip := excluded[choice.name]; skip {
				continue
			}
			if compareLoadRatio(s.active[choice.name], choice.weight, s.active[selected.name], selected.weight) != 0 {
				continue
			}
			if target == 0 {
				selected = choice
				break
			}
			target--
		}
		s.nextTie = (tieOffset + 1) % equivalentCount
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
	leftHi, leftLo := bits.Mul64(uint64(activeA), uint64(weightB))
	rightHi, rightLo := bits.Mul64(uint64(activeB), uint64(weightA))
	switch {
	case leftHi < rightHi || (leftHi == rightHi && leftLo < rightLo):
		return -1
	case leftHi > rightHi || (leftHi == rightHi && leftLo > rightLo):
		return 1
	default:
		return 0
	}
}

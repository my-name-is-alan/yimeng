package playback

import (
	"encoding/json"
	"math/rand/v2"
	"slices"
)

// Shuffle is a play-order view over the queue: the queue itself is never
// reordered. s.order is a permutation of queue indices; s.idx stays a queue
// index, and its position in s.order is the "play position".

func newSeed() int64 { return int64(rand.Uint64() >> 1) }

func rngFor(seed int64) *rand.Rand { return rand.New(rand.NewPCG(uint64(seed), 0x9e3779b97f4a7c15)) }

// reshuffle builds a fresh order with a new seed. first (if >=0) is played first.
func (s *session) reshuffle(first int) {
	s.seed = newSeed()
	s.order = shuffled(len(s.queue), first, s.seed)
}

func shuffled(n, first int, seed int64) []int {
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	r := rngFor(seed)
	r.Shuffle(n, func(i, j int) { order[i], order[j] = order[j], order[i] })
	if first >= 0 && first < n {
		p := slices.Index(order, first)
		copy(order[1:p+1], order[:p])
		order[0] = first
	}
	return order
}

// playPos is the position of the current track in play order (or the queue index when not shuffled).
func (s *session) playPos() int {
	if !s.shuffle {
		return s.idx
	}
	return slices.Index(s.order, s.idx)
}

// at maps a play position to a queue index.
func (s *session) at(p int) int {
	if !s.shuffle {
		return p
	}
	return s.order[p]
}

// nextIndex is the queue index that follows the current track, or -1.
// wrap reports that following it requires wrapping (repeat=all).
func (s *session) nextIndex(ended bool) (idx int, wrap bool) {
	n := len(s.queue)
	if n == 0 || s.idx < 0 {
		return -1, false
	}
	if ended && s.repeat == "one" {
		return s.idx, false
	}
	if p := s.playPos(); p+1 < n {
		return s.at(p + 1), false
	}
	if s.repeat == "all" {
		return s.at(0), true
	}
	return -1, false
}

// remap keeps the play order consistent after a queue edit. mapOld maps an
// old queue index to its new index (-1 = removed); added are the new queue
// indices of inserted tracks. playNext places them right after the current
// track in play order; otherwise they are spread randomly over the upcoming part.
func (s *session) remap(mapOld func(int) int, added []int, playNext bool) {
	if !s.shuffle {
		return
	}
	cur := -1 // play position of the current track, in the remapped order
	curQ := -1
	if s.idx >= 0 {
		curQ = mapOld(s.idx)
	}
	var order []int
	for _, o := range s.order {
		if n := mapOld(o); n >= 0 {
			order = append(order, n)
		}
	}
	s.order = order
	if len(added) == 0 {
		return
	}
	if curQ >= 0 {
		cur = slices.Index(s.order, curQ)
	}
	if playNext {
		s.order = slices.Insert(s.order, cur+1, added...)
		return
	}
	r := rngFor(newSeed())
	for _, a := range added {
		p := cur + 1 + r.IntN(len(s.order)-cur)
		s.order = slices.Insert(s.order, p, a)
	}
}

// validOrder guards against a corrupt persisted order.
func validOrder(order []int, n int) bool {
	if len(order) != n {
		return false
	}
	seen := make([]bool, n)
	for _, o := range order {
		if o < 0 || o >= n || seen[o] {
			return false
		}
		seen[o] = true
	}
	return true
}

func encodeOrder(s *session) any {
	if !s.shuffle {
		return nil
	}
	b, _ := json.Marshal(s.order)
	return string(b)
}

package state

// Slots models per-account concurrency: a fixed capacity, with each released
// slot held in cooldown for a (randomized, caller-supplied) duration before reuse.
type Slots struct {
	capacity int
	inUse    int
	cooling  []int64 // freeAt timestamps (ms) of released-but-cooling slots
}

// NewSlots builds a slot set with the given capacity.
func NewSlots(capacity int) *Slots {
	return &Slots{capacity: capacity}
}

// pruneCooling drops cooldown entries that have elapsed by now.
func (s *Slots) pruneCooling(now int64) {
	kept := s.cooling[:0]
	for _, t := range s.cooling {
		if t > now {
			kept = append(kept, t)
		}
	}
	s.cooling = kept
}

// Available returns how many slots can be acquired right now.
func (s *Slots) Available(now int64) int {
	s.pruneCooling(now)
	a := s.capacity - s.inUse - len(s.cooling)
	if a < 0 {
		return 0
	}
	return a
}

// Acquire claims one available slot; returns false if none is free.
func (s *Slots) Acquire(now int64) bool {
	if s.Available(now) <= 0 {
		return false
	}
	s.inUse++
	return true
}

// Release frees one in-use slot, putting it into cooldown for cooldownMs.
func (s *Slots) Release(now, cooldownMs int64) {
	if s.inUse <= 0 {
		return
	}
	s.inUse--
	if cooldownMs > 0 {
		s.cooling = append(s.cooling, now+cooldownMs)
	}
}

// InUse returns the number of currently held slots.
func (s *Slots) InUse() int { return s.inUse }

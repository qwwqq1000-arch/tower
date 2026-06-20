// Package dispatch routes requests across accounts and fallback channels.
package dispatch

// Candidate is one selectable account for WLR.
type Candidate struct {
	Key      string
	Weight   int
	Inflight int
}

func score(c Candidate) float64 {
	return float64(c.Weight) / float64(c.Inflight+1)
}

// SelectWLR picks an account by power-of-two-choices: pick2 returns two indices
// in [0,n); the higher weight/(inflight+1) score wins (lower index breaks ties).
func SelectWLR(cands []Candidate, pick2 func(n int) (i, j int)) (Candidate, bool) {
	switch len(cands) {
	case 0:
		return Candidate{}, false
	case 1:
		return cands[0], true
	}
	i, j := pick2(len(cands))
	if i < 0 || i >= len(cands) {
		i = 0
	}
	if j < 0 || j >= len(cands) {
		j = 0
	}
	a, b := cands[i], cands[j]
	if score(b) > score(a) {
		return b, true
	}
	return a, true
}

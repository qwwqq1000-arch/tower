package dispatch

import "testing"

func TestSelectWLR_Empty(t *testing.T) {
	if _, ok := SelectWLR(nil, func(n int) (int, int) { return 0, 0 }); ok {
		t.Fatal("empty candidates should return false")
	}
}

func TestSelectWLR_Single(t *testing.T) {
	c, ok := SelectWLR([]Candidate{{Key: "a", Weight: 1, Inflight: 9}}, func(n int) (int, int) { return 0, 0 })
	if !ok || c.Key != "a" {
		t.Fatalf("single candidate should be returned, got %v ok=%v", c, ok)
	}
}

func TestSelectWLR_HigherScoreWins(t *testing.T) {
	cands := []Candidate{
		{Key: "a", Weight: 10, Inflight: 9}, // score 10/10 = 1
		{Key: "b", Weight: 10, Inflight: 1}, // score 10/2 = 5
	}
	// pick2 returns indices 0 and 1
	c, ok := SelectWLR(cands, func(n int) (int, int) { return 0, 1 })
	if !ok || c.Key != "b" {
		t.Fatalf("expected b (higher score), got %v", c)
	}
}

func TestSelectWLR_WeightMatters(t *testing.T) {
	cands := []Candidate{
		{Key: "a", Weight: 100, Inflight: 1}, // 100/2 = 50
		{Key: "b", Weight: 1, Inflight: 1},   // 1/2 = 0.5
	}
	c, _ := SelectWLR(cands, func(n int) (int, int) { return 1, 0 })
	if c.Key != "a" {
		t.Fatalf("expected a (higher weight), got %v", c)
	}
}

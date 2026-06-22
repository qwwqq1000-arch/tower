package session

import "testing"

func TestAffinity_SetAndGet(t *testing.T) {
	s := NewStore()
	// No affinity initially.
	if _, ok := s.Affinity("conv1", 1000); ok {
		t.Fatal("expected no affinity initially")
	}
	// Set affinity with 5s TTL at t=1000.
	s.SetAffinity("conv1", "nodeA:default", 5000, 1000)
	if k, ok := s.Affinity("conv1", 2000); !ok || k != "nodeA:default" {
		t.Fatalf("affinity = %q,%v; want nodeA:default,true", k, ok)
	}
	// Expired after TTL.
	if _, ok := s.Affinity("conv1", 6001); ok {
		t.Fatal("affinity should have expired")
	}
}

func TestAffinity_IgnoresEmpty(t *testing.T) {
	s := NewStore()
	s.SetAffinity("", "k", 5000, 0)       // empty conv → no-op
	s.SetAffinity("c", "", 5000, 0)       // empty key → no-op
	s.SetAffinity("c", "k", 0, 0)         // zero ttl → no-op
	if _, ok := s.Affinity("c", 1); ok {
		t.Fatal("expected no affinity stored for invalid inputs")
	}
}

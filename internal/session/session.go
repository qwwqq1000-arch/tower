// Package session tracks per-conversation error streaks and exile windows so
// the dispatch layer can route misbehaving conversations to fallback channels.
package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
)

// ConvID derives a stable 16-hex-char conversation id from the request body.
// It hashes the concatenation of the system prompt and the content of the
// first user message. Returns "" if the body cannot be parsed or carries no
// usable signal.
func ConvID(body []byte) string {
	var req struct {
		System   any `json:"system"`
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}

	// stringify system
	var system string
	switch v := req.System.(type) {
	case string:
		system = v
	case nil:
		// leave empty
	default:
		// array or object — JSON-encode it
		b, _ := json.Marshal(v)
		system = string(b)
	}

	// find first user message content
	var firstUser string
	for _, m := range req.Messages {
		if m.Role == "user" {
			switch v := m.Content.(type) {
			case string:
				firstUser = v
			default:
				b, _ := json.Marshal(v)
				firstUser = string(b)
			}
			break
		}
	}

	if system == "" && firstUser == "" {
		return ""
	}

	h := sha256.Sum256([]byte(system + "\x00" + firstUser))
	return hex.EncodeToString(h[:])[:16]
}

// matchesAny reports whether body contains any of the keywords (case-insensitive).
func matchesAny(body string, kws []string) bool {
	lower := strings.ToLower(body)
	for _, kw := range kws {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

type state struct {
	errs        int
	exiledUntil int64
}

// Store tracks per-conversation error counts and exile windows in memory.
type Store struct {
	mu sync.Mutex
	m  map[string]*state
}

// NewStore creates a new, empty Store.
func NewStore() *Store {
	return &Store{m: make(map[string]*state)}
}

func (s *Store) get(conv string) *state {
	st, ok := s.m[conv]
	if !ok {
		st = &state{}
		s.m[conv] = st
	}
	return st
}

// Exiled reports whether the conversation is currently exiled.
// Returns false if conv is "".
func (s *Store) Exiled(conv string, now int64) bool {
	if conv == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[conv]
	if !ok {
		return false
	}
	return now < st.exiledUntil
}

// RecordError increments the error counter for conv and triggers exile if
// the error count reaches threshold (threshold=0 means disabled).
func (s *Store) RecordError(conv string, threshold, cooldownMs, now int64) {
	if conv == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.get(conv)
	st.errs++
	if threshold > 0 && int64(st.errs) >= threshold {
		st.exiledUntil = now + cooldownMs
		st.errs = 0
	}
}

// RecordSuccess resets the error counter and clears any exile window.
func (s *Store) RecordSuccess(conv string) {
	if conv == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.get(conv)
	st.errs = 0
	st.exiledUntil = 0
}

// ForceExile immediately exiles the conversation for cooldownMs milliseconds.
func (s *Store) ForceExile(conv string, cooldownMs, now int64) {
	if conv == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.get(conv)
	st.exiledUntil = now + cooldownMs
}

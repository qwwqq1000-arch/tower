package dispatch

import "encoding/json"

// padBody injects nBytes of padding into the body's metadata.pad field.
//
// Safety vector: Anthropic's Messages API treats the metadata object as a
// pass-through store for caller-defined keys — documented keys are user_id etc.,
// but the API accepts and ignores arbitrary extra keys within metadata.
// Injecting into metadata.pad is therefore the safest vector: it does not change
// any API-meaningful field and does not risk a 400 (unlike top-level unknown keys
// which are rejected by strict upstream validation).
//
// NOTE: This function is intentionally conservative. The injection vector was
// selected based on documented Anthropic API behaviour. Verify against a real
// upstream endpoint before enabling BodyPadEnabled in production — see task brief
// Step 1. Until verified and BodyPadEnabled=true + BodyPadBytes>{0,0}, this code
// path is never reached (guarded by the callers in Dispatch/DispatchStream).
//
// On ANY error (unmarshal, marshal), returns the original body unchanged so that
// padding failure never fails a real request.
func padBody(body []byte, nBytes int, seed string) []byte {
	if nBytes <= 0 {
		return body
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	// Preserve existing metadata; create an empty map if absent or wrong type.
	meta, ok := m["metadata"].(map[string]any)
	if !ok {
		meta = map[string]any{}
	}
	// Generate varied-but-deterministic padding using seed to shift the alphabet
	// window, so requests from different conversations get different pad content.
	pad := make([]byte, nBytes)
	seedLen := len(seed)
	for i := range pad {
		offset := i
		if seedLen > 0 {
			offset += int(seed[i%seedLen])
		}
		pad[i] = 'a' + byte(offset%26)
	}
	meta["pad"] = string(pad)
	m["metadata"] = meta

	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

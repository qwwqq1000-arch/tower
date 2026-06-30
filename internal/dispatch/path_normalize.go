package dispatch

import (
	"bytes"
	"context"
	"regexp"
	"strings"
)

type ctxKeyHomeDir struct{}

func withHomeDir(ctx context.Context, homeDir string) context.Context {
	if homeDir == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyHomeDir{}, homeDir)
}

func homeDirFrom(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyHomeDir{}).(string)
	return v
}

var reHomeDir = regexp.MustCompile(`/(?:home|Users)/([^\\/\s"]+)`)

// extractHomeDir finds the user's home directory prefix from the request body.
// Returns the full prefix (e.g. "/Users/leo", "/home/peter") or "".
// Skips "/home/user" (already normalized) and "/home/openclaw" (shared service account).
func extractHomeDir(body []byte) string {
	m := reHomeDir.FindSubmatch(body)
	if m == nil {
		return ""
	}
	username := string(m[1])
	if username == "user" || username == "openclaw" {
		return ""
	}
	return string(m[0])
}

// normalizePathsOut applies all identity-stripping transformations to the outgoing
// request body. Returns the modified body and the extracted homeDir for response
// denormalization. Covers:
//   - Home dir paths (/Users/xxx, /home/xxx) → /home/user     (P0 #1,2,4,6,16,17)
//   - userEmail line                         → deleted         (P0 #8)
//   - Scratchpad path (UID + work-slug + UUID) → normalized    (P1 #13)
//   - metadata.user_id (device_id, session_id) → fixed value   (P2 #22)
func normalizePathsOut(body []byte) ([]byte, string) {
	homeDir := extractHomeDir(body)
	if homeDir == "" {
		return body, ""
	}
	s := string(body)

	// #1,2,4,6,16,17: replace all home dir occurrences across entire body
	// (system prompt + messages + tool results).
	s = strings.ReplaceAll(s, homeDir, "/home/user")

	// #8: strip userEmail line.
	s = reUserEmail.ReplaceAllString(s, "")

	// #13: scratchpad path contains UID, work-dir slug, session UUID.
	// /private/tmp/claude-501/-Users-leo-project/UUID/scratchpad → /tmp/scratchpad
	s = reScratchpad.ReplaceAllString(s, "/tmp/scratchpad")

	// #22: metadata.user_id contains device_id, session_id, account_uuid.
	// Replace the entire user_id value with a fixed one.
	s = reMetadataUserID.ReplaceAllString(s, `"user_id":"u"`)

	// #10-12: OS/Platform/Shell — normalize to linux/bash.
	s = rePlatform.ReplaceAllString(s, "Platform: linux")
	s = reShell.ReplaceAllString(s, "Shell: bash")
	s = reOSVersion.ReplaceAllString(s, "OS Version: Linux 6.1.0")

	return []byte(s), homeDir
}

// userEmail: matches "# userEmail\nThe user's email...\n" in system prompt
var reUserEmail = regexp.MustCompile(`(?m)# userEmail\n[^\n]*\n`)

// scratchpad: /private/tmp/claude-{UID}/{slug}/{UUID}/scratchpad
var reScratchpad = regexp.MustCompile(`/private/tmp/claude-\d+/[^/\s"]+/[0-9a-f-]+/scratchpad`)

// metadata.user_id: "user_id":"{\"device_id\":\"...\",\"session_id\":\"...\"}"
// The inner value is JSON-encoded string with escaped quotes, or a plain string.
// Match "user_id":"..." handling escaped quotes inside.
var reMetadataUserID = regexp.MustCompile(`"user_id"\s*:\s*"(?:[^"\\]|\\.)*"`)

// OS/Platform/Shell: match the "- Platform: xxx" lines in system prompt.
// These appear as "Platform: darwin\n" in the JSON-encoded system prompt text.
var rePlatform = regexp.MustCompile(`Platform: (?:darwin|win32)`)
var reShell = regexp.MustCompile(`Shell: (?:zsh|PowerShell|powershell|PowerShell \([^)]*\))`)
var reOSVersion = regexp.MustCompile(`OS Version: (?:Darwin|Windows|MasOS|macOS)[^\n\\]*`)

// denormalizeStr replaces /home/user back to the real home dir in a response string.
func denormalizeStr(text, homeDir string) string {
	if homeDir == "" {
		return text
	}
	return strings.ReplaceAll(text, "/home/user", homeDir)
}

// denormalizeBytes replaces /home/user back to the real home dir in a byte slice.
func denormalizeBytes(data []byte, homeDir string) []byte {
	if homeDir == "" {
		return data
	}
	return bytes.ReplaceAll(data, []byte("/home/user"), []byte(homeDir))
}

package dispatch

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func testServiceCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	c, err := crypto.NewCipher(base64.StdEncoding.EncodeToString(k))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestChannelApiKey_DecryptedBeforeForward is the channel round-trip for
// vault-crypto-3: a fallback channel's api_key is stored encrypted at rest and
// the dispatch service decrypts it (via s.Cipher.DecryptOrPlaintext) before
// building the ChannelProxy, so the upstream relay receives the plaintext key in
// x-api-key. This mirrors the exact decrypt-then-forward step in viaChannel /
// streamChannel without requiring a full DB-backed dispatch.
func TestChannelApiKey_DecryptedBeforeForward(t *testing.T) {
	const plaintextKey = "sk-channel-plaintext-xyz"
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		_, _ = w.Write([]byte(`{"relayed":true}`))
	}))
	defer srv.Close()

	svc := &Service{Cipher: testServiceCipher(t)}

	// Stored at rest: ciphertext (encrypt-on-write).
	encKey := svc.Cipher.EncryptStr(plaintextKey)
	if encKey == plaintextKey {
		t.Fatal("channel api_key was not encrypted")
	}
	ch := sqlc.FallbackChannel{ID: "fc_1", BaseUrl: srv.URL, ApiKey: encKey}

	// The decrypt-on-read → use step, identical to viaChannel/streamChannel.
	cp := &ChannelProxy{Body: []byte(`{}`), Ch: ChannelRef{BaseURL: ch.BaseUrl, APIKey: svc.Cipher.DecryptOrPlaintext(ch.ApiKey)}}
	res, err := cp.Send(context.Background(), ch.ID)
	if err != nil || res.Status != 200 {
		t.Fatalf("channel send: res=%+v err=%v", res, err)
	}
	if gotKey != plaintextKey {
		t.Fatalf("upstream x-api-key = %q, want decrypted plaintext %q", gotKey, plaintextKey)
	}
}

// TestChannelApiKey_LegacyPlaintext: a legacy plaintext api_key (un-migrated)
// still forwards correctly through the transparent read shim.
func TestChannelApiKey_LegacyPlaintext(t *testing.T) {
	const plaintextKey = "sk-legacy-channel"
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		_, _ = w.Write([]byte(`{"relayed":true}`))
	}))
	defer srv.Close()

	svc := &Service{Cipher: testServiceCipher(t)}
	ch := sqlc.FallbackChannel{ID: "fc_legacy", BaseUrl: srv.URL, ApiKey: plaintextKey}

	cp := &ChannelProxy{Body: []byte(`{}`), Ch: ChannelRef{BaseURL: ch.BaseUrl, APIKey: svc.Cipher.DecryptOrPlaintext(ch.ApiKey)}}
	if _, err := cp.Send(context.Background(), ch.ID); err != nil {
		t.Fatalf("legacy channel send: %v", err)
	}
	if gotKey != plaintextKey {
		t.Fatalf("upstream x-api-key = %q, want %q", gotKey, plaintextKey)
	}
}

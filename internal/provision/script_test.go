package provision

import (
	"strings"
	"testing"
)

func TestGenAPIKey(t *testing.T) {
	k := GenAPIKey(func(b []byte) {
		for i := range b {
			b[i] = 0xab
		}
	})
	if !strings.HasPrefix(k, "sk-mer-") {
		t.Fatalf("key=%q", k)
	}
	if len(k) != len("sk-mer-")+64 {
		t.Fatalf("len=%d", len(k))
	}
}

func TestSteps(t *testing.T) {
	steps := Steps(Input{APIKey: "sk-mer-abc", FingerprintSeed: "seed123", SourceRepo: "https://github.com/qwwqq1000-arch/new-meridian", InstallDir: "/opt/meridian"})
	keys := make([]string, len(steps))
	for i, s := range steps {
		keys[i] = s.Key
	}
	want := []string{"ensure-docker", "fetch-source", "write-env", "build", "compose-up", "read-key"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("step order=%v want=%v", keys, want)
	}
	all := ""
	for _, s := range steps {
		all += s.Cmd + "\n"
	}
	if !strings.Contains(all, "seed123") {
		t.Fatal("build must use fingerprint seed as CACHEBUST")
	}
	if !strings.Contains(all, "new-meridian") {
		t.Fatal("fetch-source must clone the repo")
	}
	if !strings.Contains(all, "sk-mer-abc") {
		t.Fatal("write-env must set the api key")
	}
	if !strings.Contains(all, "/opt/meridian") {
		t.Fatal("commands use install dir")
	}
}

package fallback

import "testing"

func base() DecideInput {
	return DecideInput{Model: "claude-opus-4-8", BodyText: "please refactor this large module ...", EstCostUsd: 1.0, PriceThresholdUsd: 0.005}
}

func TestDecide_None(t *testing.T) {
	if g := Decide(base()); g != None {
		t.Fatalf("got %v, want None", g)
	}
}

func TestDecide_KeywordHighestPriority(t *testing.T) {
	in := base()
	in.Keywords = []string{"refactor"}
	in.EstCostUsd = 0.0001 // would also be price, but keyword wins
	if g := Decide(in); g != Keyword {
		t.Fatalf("got %v, want Keyword", g)
	}
}

func TestDecide_Model(t *testing.T) {
	in := base()
	in.FallbackModels = []string{"opus-4-8"}
	if g := Decide(in); g != Model {
		t.Fatalf("got %v, want Model", g)
	}
}

func TestDecide_Probe(t *testing.T) {
	in := base()
	in.ProbeText = "hi"
	in.ProbeEnabled = true
	if g := Decide(in); g != Probe {
		t.Fatalf("got %v, want Probe", g)
	}
}

func TestDecide_ProbeOnFullJSONBody(t *testing.T) {
	// Simulates the real scenario: BodyText is the raw request JSON,
	// ProbeText is the extracted last user message text.
	fullJSON := `{"model":"claude-opus-4-8","messages":[{"role":"user","content":"hi"}]}`
	in := base()
	in.BodyText = fullJSON  // keyword matching still uses raw JSON
	in.ProbeText = "hi"     // extracted user text triggers probe
	in.ProbeEnabled = true
	if g := Decide(in); g != Probe {
		t.Fatalf("full JSON body with user content 'hi': got %v, want Probe", g)
	}
}

func TestDecide_ProbeNotTriggeredOnLongUserMessage(t *testing.T) {
	in := base()
	in.ProbeText = "please explain how photosynthesis works in detail"
	in.ProbeEnabled = true
	if g := Decide(in); g == Probe {
		t.Fatalf("long user message should not trigger probe, got Probe")
	}
}

func TestDecide_Price(t *testing.T) {
	in := base()
	in.EstCostUsd = 0.001 // below 0.005
	if g := Decide(in); g != Price {
		t.Fatalf("got %v, want Price", g)
	}
}

func TestDecide_Exhausted(t *testing.T) {
	in := base()
	in.PoolEmpty = true
	if g := Decide(in); g != Exhausted {
		t.Fatalf("got %v, want Exhausted", g)
	}
}

func TestDecide_PriceBeatsExhausted(t *testing.T) {
	in := base()
	in.EstCostUsd = 0.001
	in.PoolEmpty = true
	if g := Decide(in); g != Price {
		t.Fatalf("got %v, want Price (higher priority than Exhausted)", g)
	}
}

func TestIsProbe_CJK(t *testing.T) {
	if !IsProbe("测活") {
		t.Fatal("测活 (a bundled probe word) must be detected")
	}
	if IsProbe("这是一段很长的中文内容需要真实处理而不是探活") {
		t.Fatal("long CJK content must not be treated as probe")
	}
}

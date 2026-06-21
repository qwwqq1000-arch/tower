package reconcile

import (
	"context"
	"testing"
)

type fakeAPI struct {
	actual  map[string]map[string]any
	patched map[string]map[string]any
	getErr  error
}

func (f *fakeAPI) GetFeatures(_ context.Context) (map[string]map[string]any, error) {
	return f.actual, f.getErr
}
func (f *fakeAPI) PatchFeatures(_ context.Context, adapter string, patch map[string]any) error {
	if f.patched == nil {
		f.patched = map[string]map[string]any{}
	}
	f.patched[adapter] = patch
	return nil
}

func TestReconcileNode_HealsDrift(t *testing.T) {
	desired := map[string]map[string]any{"opencode": {"memory": true}}
	api := &fakeAPI{actual: map[string]map[string]any{"opencode": {"memory": false}}}
	n, err := ReconcileNode(context.Background(), api, desired)
	if err != nil || n != 1 {
		t.Fatalf("patched=%d err=%v", n, err)
	}
	if api.patched["opencode"]["memory"] != true {
		t.Fatalf("should patch memory:true, got %v", api.patched["opencode"])
	}
}

func TestReconcileNode_NoDriftNoPatch(t *testing.T) {
	desired := map[string]map[string]any{"opencode": {"memory": true}}
	api := &fakeAPI{actual: map[string]map[string]any{"opencode": {"memory": true}}}
	n, _ := ReconcileNode(context.Background(), api, desired)
	if n != 0 || api.patched != nil {
		t.Fatal("no drift → no patch")
	}
}

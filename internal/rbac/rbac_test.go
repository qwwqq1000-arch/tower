package rbac

import "testing"

func TestCan_Wildcard(t *testing.T) {
	if !Can([]string{"*"}, "billing:settle", Scope{}) {
		t.Fatal("wildcard should grant any capability regardless of scope")
	}
}

func TestCan_CapabilityAndScope(t *testing.T) {
	perms := []string{"nodes:read", "nodes:write"}
	// has cap + owner match → allow
	if !Can(perms, "nodes:write", Scope{OwnerMatch: true}) {
		t.Fatal("cap+owner should allow")
	}
	// has cap but no scope → deny
	if Can(perms, "nodes:write", Scope{}) {
		t.Fatal("cap without scope should deny")
	}
	// missing cap → deny even with admin scope
	if Can(perms, "billing:settle", Scope{IsAdmin: true}) {
		t.Fatal("missing cap should deny")
	}
	// admin scope + cap present → allow
	if !Can(perms, "nodes:read", Scope{IsAdmin: true}) {
		t.Fatal("admin+cap should allow")
	}
	// group operator scope + cap → allow
	if !Can(perms, "nodes:read", Scope{GroupOperator: true}) {
		t.Fatal("groupOperator+cap should allow")
	}
}

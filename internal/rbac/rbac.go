// Package rbac evaluates capability-based authorization with ownership/group scope.
package rbac

// Scope describes the requesting user's relationship to the target resource.
type Scope struct {
	IsAdmin       bool // superadmin/admin: sees everything
	OwnerMatch    bool // resource.owner_id == user
	GroupOperator bool // user is an operator in the resource's group
}

// Can reports whether a role holding perms may perform capability under sc.
// "*" in perms grants everything. Otherwise the capability must be present
// AND the scope must permit acting on the resource.
func Can(perms []string, capability string, sc Scope) bool {
	has := false
	for _, p := range perms {
		if p == "*" {
			return true
		}
		if p == capability {
			has = true
		}
	}
	if !has {
		return false
	}
	return sc.IsAdmin || sc.OwnerMatch || sc.GroupOperator
}

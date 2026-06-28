package identity

// Role is a named bundle of permissions. Roles live in the database; the seeded
// system roles are defaults, not a closed set — admins create custom roles too.
type Role struct {
	ID          RoleID
	Name        string
	IsSystem    bool
	Permissions []Permission
}

// Has reports whether the role grants p.
func (r Role) Has(p Permission) bool {
	for _, x := range r.Permissions {
		if x == p {
			return true
		}
	}
	return false
}

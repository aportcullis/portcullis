package identity

// Member is a user seen through its membership in one organization: the account, its single assigned role and whether a password is set (ADR-0053).
type Member struct {
	User        User
	RoleID      RoleID
	RoleName    string
	HasPassword bool
}

// NewMember is the account and initial role an administrator creates.
type NewMember struct {
	Email       string
	DisplayName string
	RoleID      RoleID
}

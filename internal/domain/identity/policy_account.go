package identity

// PolicyAccount describes independent organization membership without a role assignment.
type PolicyAccount struct {
	User        User
	HasPassword bool
}

// NewPolicyAccount contains the account data needed to establish membership without grants.
type NewPolicyAccount struct {
	Email       string
	DisplayName string
}

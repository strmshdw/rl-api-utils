package auth

// AccountRole identifies whether an auth provider serves the primary playing account
// or the secondary non-playing polling account.
type AccountRole string

const (
	RolePrimary AccountRole = "primary"
	RolePolling AccountRole = "polling"
)

// Label returns the human-readable identifier for terminal prompts and logs.
func (r AccountRole) Label() string {
	if r == RolePolling {
		return "Secondary Polling Account"
	}
	return "Primary Account"
}

// StoreKey returns the unique persistence key in StateStore (auth_state table)
// ensuring complete isolation between primary and polling credentials.
func (r AccountRole) StoreKey(provider string) string {
	if r == RolePolling {
		return provider + "_polling"
	}
	return provider
}

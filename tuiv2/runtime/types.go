package runtime

type BindingRole string

const (
	BindingRoleOwner    BindingRole = "owner"
	BindingRoleFollower BindingRole = "follower"
)

type StreamState struct{}
type RecoveryState struct{}

type VTermLike interface{}

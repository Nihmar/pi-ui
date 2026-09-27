//go:build !unix

package sessions

// defaultIsolationUser is empty on a platform without uid:gid: the container runtime decides
// the identity, which is the honest answer when there is no number to pass.
func defaultIsolationUser() string { return "" }

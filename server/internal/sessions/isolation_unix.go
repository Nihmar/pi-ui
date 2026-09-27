//go:build unix

package sessions

import (
	"fmt"
	"os"
)

// defaultIsolationUser is this process's uid:gid, so the bind mounts a container writes
// through have the same owner as the host files.
func defaultIsolationUser() string {
	return fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
}

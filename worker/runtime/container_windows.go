//go:build windows

package runtime

import (
	bespec "github.com/concourse/concourse/worker/runtime/spec"
)

func processCwd(dir string) string {
	if dir == "" {
		return `C:\`
	}
	return bespec.WindowsContainerPath(dir)
}

// envWithDefaultPath never injects a PATH on Windows; the image's own
// environment applies.
func envWithDefaultPath(uid uint32, currentEnv []string) string {
	return ""
}

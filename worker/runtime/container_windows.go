//go:build windows

package runtime

const defaultProcessCwd = `C:\`

// envWithDefaultPath never injects a PATH on Windows; the image's own
// environment applies.
func envWithDefaultPath(uid uint32, currentEnv []string) string {
	return ""
}

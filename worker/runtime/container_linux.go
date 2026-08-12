//go:build linux

package runtime

import (
	"regexp"
	"slices"
)

const (
	SuperuserPath = "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	Path          = "PATH=/usr/local/bin:/usr/bin:/bin"

	defaultProcessCwd = "/"
)

var pathRegexp = regexp.MustCompile("^PATH=.*$")

// Set a default path based on the UID if no existing PATH is found
func envWithDefaultPath(uid uint32, currentEnv []string) string {
	pathFound := slices.ContainsFunc(currentEnv, pathRegexp.MatchString)
	if pathFound {
		return ""
	}

	if uid == 0 {
		return SuperuserPath
	}

	return Path
}

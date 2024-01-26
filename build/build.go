package build

import "runtime/debug"

var (
	tag      string
	revision string
)

func GetRevision() string {
	if revision != "" {
		return revision
	}

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}

	for _, setting := range buildInfo.Settings {
		if setting.Key == "vcs.revision" {
			revision = setting.Value
			return revision
		}
	}

	return "unknown"
}

func GetTag() string {
	if tag != "" {
		return tag
	}

	return "none"
}

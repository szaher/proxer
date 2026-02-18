package nativeagent

import "strings"

var (
	version   = "dev"
	commitSHA = ""
	buildDate = ""
)

type BuildInfo struct {
	Version   string `json:"version"`
	CommitSHA string `json:"commit_sha,omitempty"`
	BuildDate string `json:"build_date,omitempty"`
}

func BuildVersion() string {
	if strings.TrimSpace(version) == "" {
		return "dev"
	}
	return strings.TrimSpace(version)
}

func CurrentBuildInfo() BuildInfo {
	return BuildInfo{
		Version:   BuildVersion(),
		CommitSHA: strings.TrimSpace(commitSHA),
		BuildDate: strings.TrimSpace(buildDate),
	}
}

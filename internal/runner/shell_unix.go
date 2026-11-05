//go:build !windows

package runner

func shellCandidates() []string { return []string{"sh"} }

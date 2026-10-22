//go:build !windows

package verify

func shellCandidates() []string { return []string{"sh"} }

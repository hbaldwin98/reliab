package runner

import (
	"errors"
	"os/exec"
)

// FindShell returns the POSIX sh that reliab.yaml commands run under.
func FindShell() (string, error) {
	for _, c := range shellCandidates() {
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	return "", errors.New("no POSIX sh found; reliab.yaml commands run under sh (on Windows install Git for Windows)")
}

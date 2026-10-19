package report

import "testing"

// knowledge/decisions/finding-classification.md ("A command-level finding
// carries only while its failure is the same") and the Fingerprint doc
// comment: the fingerprint hashes the message and observed output with the
// repository root removed, listed apart from "other absolute paths". So the
// same failure in a checkout at another root has the same fingerprint, and
// removing only the root keeps the repo-relative path: a failure in another
// file of the repository is a different failure.
func TestFingerprintRemovesTheRepositoryRoot(t *testing.T) {
	at := func(root, file string) Finding {
		return Finding{
			Message:  "open " + root + "/" + file + ": permission denied",
			Observed: root + "/" + file + " is read-only",
		}
	}
	a, b := "/work/checkout-a", "/home/ci/checkout-b"
	if fa, fb := Fingerprint(at(a, "x.go"), a), Fingerprint(at(b, "x.go"), b); fa != fb {
		t.Errorf("the same failure under two roots: %s != %s", fa, fb)
	}
	if fx, fy := Fingerprint(at(a, "x.go"), a), Fingerprint(at(a, "y.go"), a); fx == fy {
		t.Errorf("failures in x.go and y.go share fingerprint %s", fx)
	}
	if nx, ny := Normalize(at(a, "x.go").Message, a), Normalize(at(a, "y.go").Message, a); nx == ny {
		t.Errorf("Normalize lost the repo-relative path: %q", nx)
	}
}

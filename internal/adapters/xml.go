package adapters

import (
	"strings"

	"github.com/hbaldwin98/reliab/pkg/testresults"
)

// ParseJUnit returns the failing and erroring test cases in a JUnit report
// (pkg/testresults, which the in-repo adapters use too).
func ParseJUnit(raw []byte) ([]Failure, error) { return testresults.ParseJUnit(raw) }

// ParseTRX returns the failed results in a TRX report (pkg/testresults).
func ParseTRX(raw []byte) ([]Failure, error) { return testresults.ParseTRX(raw) }

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

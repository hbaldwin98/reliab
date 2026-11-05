// Package report defines a finding: one failure, survivor or issue a tool
// reported, in the one shape every adapter writes
// (knowledge/decisions/profile-v2.md, "Findings").
package report

// Finding kinds. A tool's finding starts unclassified; the three kinds are
// what the person or agent acting on it decides.
const (
	KindUnclassified = "unclassified"
	// KindFixCode: the product code is wrong.
	KindFixCode = "fix-code"
	// KindFixTest: the test or harness is wrong, or a test is missing.
	KindFixTest = "fix-test"
	// KindAskHuman: only a person can say whether the behavior is intended.
	KindAskHuman = "ask-human"
)

// StatusOpen is a finding no one has dealt with yet.
const StatusOpen = "open"

// Finding is one thing a command reported.
type Finding struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Command      string `json:"command"`
	Test         string `json:"test,omitempty"`
	Location     string `json:"location,omitempty"`
	Intent       string `json:"intent,omitempty"`
	Replay       string `json:"replay,omitempty"`
	MinimalInput any    `json:"minimalInput,omitempty"`
	Observed     string `json:"observed,omitempty"`
	Expected     string `json:"expected,omitempty"`
	Message      string `json:"message,omitempty"`
	Diff         string `json:"diff,omitempty"`
	Status       string `json:"status"`
}

// Percent is killed over counted, in percent; 0 when nothing was counted.
func Percent(killed, counted int) float64 {
	if counted == 0 {
		return 0
	}
	return 100 * float64(killed) / float64(counted)
}

// Below reports whether killed of counted falls short of want percent.
// Nothing counted is never below: there is nothing to hold.
func Below(killed, counted int, want float64) bool {
	return counted > 0 && 100*float64(killed) < want*float64(counted)
}

// LineCounts is how many lines a coverage report measured and how many
// tests reached. ChangedSince is set when they count only lines changed
// since that base ref.
type LineCounts struct {
	Valid        int    `json:"valid"`
	Covered      int    `json:"covered"`
	ChangedSince string `json:"changedSince,omitempty"`
}

// MutantCounts is how many mutants a mutation command killed (timeouts
// included), how many survived, and how many no test covers.
type MutantCounts struct {
	Killed     int `json:"killed"`
	Survived   int `json:"survived"`
	NotCovered int `json:"notCovered,omitempty"`
	// ChangedSince is set when the counts cover only mutants on lines
	// changed since that base ref.
	ChangedSince string `json:"changedSince,omitempty"`
}

// FileCounts are one file's mutant counts.
type FileCounts struct {
	Killed     int `json:"killed"`
	Survived   int `json:"survived"`
	NotCovered int `json:"notCovered,omitempty"`
}

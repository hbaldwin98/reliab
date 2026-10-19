package cli

import "errors"

// Exit codes, per knowledge/decisions/tiers-budgets-exit-codes.md.
const (
	ExitOK         = 0 // passed and complete
	ExitFindings   = 1 // open findings
	ExitConfig     = 2 // config or usage error
	ExitIncomplete = 3 // incomplete (budget) with no findings
)

// exitHookError is the exit code for a hook that failed to decide. Claude
// Code shows exit 1 as a non-blocking error; exit 2 would block the action.
const exitHookError = 1

// exitError carries a specific exit code out of a command.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

func withCode(code int, err error) error { return &exitError{code: code, err: err} }

// codeOf maps an error returned by a command to a process exit code.
// Errors without an explicit code are usage or config errors.
func codeOf(err error) int {
	if err == nil {
		return ExitOK
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return ExitConfig
}

// Package hookio holds Claude Code's hook protocol: the JSON a hook reads on
// stdin and the JSON it writes to decide. Nothing else in reliab knows this
// format; see knowledge/decisions/claude-code-first.md.
package hookio

import (
	"encoding/json"
	"fmt"
	"io"
)

// Input is the subset of a hook's stdin that reliab reads.
type Input struct {
	SessionID      string    `json:"session_id"`
	Cwd            string    `json:"cwd"`
	HookEventName  string    `json:"hook_event_name"`
	ToolName       string    `json:"tool_name"`
	ToolInput      ToolInput `json:"tool_input"`
	StopHookActive bool      `json:"stop_hook_active"`
	Source         string    `json:"source"` // SessionStart: startup, resume, clear, compact
	// AgentType names the subagent making the call, as plugin:name for a
	// plugin's agent; empty in the main session.
	AgentType string `json:"agent_type"`
}

// ToolInput holds the tool arguments hooks inspect.
type ToolInput struct {
	FilePath     string `json:"file_path"`     // Edit, Write
	NotebookPath string `json:"notebook_path"` // NotebookEdit
	Command      string `json:"command"`       // Bash, PowerShell
	// The text an edit removes and adds, for protected_edits.
	OldString string     `json:"old_string"` // Edit
	NewString string     `json:"new_string"` // Edit
	Content   string     `json:"content"`    // Write
	NewSource string     `json:"new_source"` // NotebookEdit
	Edits     []TextEdit `json:"edits"`      // MultiEdit
}

// TextEdit is one replacement in a multi-edit call.
type TextEdit struct {
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

// Path returns the file a file-editing tool targets.
func (t ToolInput) Path() string {
	if t.FilePath != "" {
		return t.FilePath
	}
	return t.NotebookPath
}

// Read decodes hook input.
func Read(r io.Reader) (Input, error) {
	var in Input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return in, fmt.Errorf("reading hook input: %w", err)
	}
	return in, nil
}

// Decision is what reliab decided, independent of the protocol.
type Decision struct {
	Deny    string // PreToolUse: deny with this reason
	Block   string // Stop: keep the agent working, with this reason
	Context string // SessionStart: add this to the agent's context
}

// Write encodes d for event. An empty decision writes nothing, which the
// harness reads as allow.
func Write(w io.Writer, event string, d Decision) error {
	var out any
	switch {
	case d.Deny != "":
		out = map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName":            event,
			"permissionDecision":       "deny",
			"permissionDecisionReason": d.Deny,
		}}
	case d.Block != "":
		out = map[string]any{"decision": "block", "reason": d.Block}
	case d.Context != "":
		out = map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName":     event,
			"additionalContext": d.Context,
		}}
	default:
		return nil
	}
	return json.NewEncoder(w).Encode(out)
}

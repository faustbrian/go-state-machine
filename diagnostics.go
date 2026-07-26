package statemachine

import (
	"fmt"
	"strings"
)

// DiagnosticCode identifies a definition defect without requiring callers to
// parse error text.
type DiagnosticCode string

const (
	DiagnosticMissingVersion      DiagnosticCode = "missing_version"
	DiagnosticMissingInitial      DiagnosticCode = "missing_initial"
	DiagnosticDuplicateState      DiagnosticCode = "duplicate_state"
	DiagnosticUnknownState        DiagnosticCode = "unknown_state"
	DiagnosticMissingTransitionID DiagnosticCode = "missing_transition_id"
	DiagnosticDuplicateTransition DiagnosticCode = "duplicate_transition_id"
	DiagnosticMissingSource       DiagnosticCode = "missing_source"
	DiagnosticInvalidWildcard     DiagnosticCode = "invalid_wildcard"
	DiagnosticAmbiguousTransition DiagnosticCode = "ambiguous_transition"
	DiagnosticAmbiguousWildcard   DiagnosticCode = "ambiguous_wildcard"
	DiagnosticTerminalTransition  DiagnosticCode = "terminal_transition"
	DiagnosticUnreachableState    DiagnosticCode = "unreachable_state"
	DiagnosticMissingEffectKind   DiagnosticCode = "missing_effect_kind"
	DiagnosticLimitExceeded       DiagnosticCode = "limit_exceeded"
)

// Diagnostic describes one defect discovered while compiling a definition.
type Diagnostic struct {
	Code         DiagnosticCode
	Message      string
	TransitionID TransitionID
}

// DiagnosticsError contains every independently detectable compile defect.
type DiagnosticsError struct {
	Diagnostics []Diagnostic
}

func (err *DiagnosticsError) Error() string {
	messages := make([]string, len(err.Diagnostics))
	for index, diagnostic := range err.Diagnostics {
		messages[index] = fmt.Sprintf("%s: %s", diagnostic.Code, diagnostic.Message)
	}
	return "statemachine: invalid definition: " + strings.Join(messages, "; ")
}

// Has reports whether the collection includes code.
func (err *DiagnosticsError) Has(code DiagnosticCode) bool {
	for _, diagnostic := range err.Diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

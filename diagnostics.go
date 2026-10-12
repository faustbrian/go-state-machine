package statemachine

// DiagnosticCode identifies a definition defect without requiring callers to
// parse error text.
type DiagnosticCode string

const (
	// DiagnosticMissingVersion reports an empty definition version.
	DiagnosticMissingVersion DiagnosticCode = "missing_version"
	// DiagnosticMissingInitial reports an initial state absent from the definition.
	DiagnosticMissingInitial DiagnosticCode = "missing_initial"
	// DiagnosticDuplicateState reports repeated state identifiers.
	DiagnosticDuplicateState DiagnosticCode = "duplicate_state"
	// DiagnosticUnknownState reports an undefined transition source or destination.
	DiagnosticUnknownState DiagnosticCode = "unknown_state"
	// DiagnosticMissingTransitionID reports an empty transition ID.
	DiagnosticMissingTransitionID DiagnosticCode = "missing_transition_id"
	// DiagnosticDuplicateTransition reports repeated transition IDs.
	DiagnosticDuplicateTransition DiagnosticCode = "duplicate_transition_id"
	// DiagnosticMissingSource reports an exact transition without sources.
	DiagnosticMissingSource DiagnosticCode = "missing_source"
	// DiagnosticInvalidWildcard reports a wildcard with explicit sources.
	DiagnosticInvalidWildcard DiagnosticCode = "invalid_wildcard"
	// DiagnosticAmbiguousTransition reports multiple exact matches for a state/event pair.
	DiagnosticAmbiguousTransition DiagnosticCode = "ambiguous_transition"
	// DiagnosticAmbiguousWildcard reports multiple wildcards for the same event.
	DiagnosticAmbiguousWildcard DiagnosticCode = "ambiguous_wildcard"
	// DiagnosticTerminalTransition reports an exact outgoing edge from a terminal state.
	DiagnosticTerminalTransition DiagnosticCode = "terminal_transition"
	// DiagnosticUnreachableState reports graph-unreachable states, without evaluating guard eligibility.
	DiagnosticUnreachableState DiagnosticCode = "unreachable_state"
	// DiagnosticMissingEffectKind reports an empty planned effect kind.
	DiagnosticMissingEffectKind DiagnosticCode = "missing_effect_kind"
	// DiagnosticLimitExceeded reports invalid limits or rejected collection/payload budgets.
	DiagnosticLimitExceeded DiagnosticCode = "limit_exceeded"
)

// Diagnostic describes one defect discovered while compiling a definition.
type Diagnostic struct {
	// Code classifies the construction defect without parsing Message.
	Code DiagnosticCode
	// Message may format caller-supplied state identifiers; it is not universally redacted.
	Message string
	// TransitionID identifies the affected transition when applicable; otherwise empty.
	TransitionID TransitionID
}

// DiagnosticsError contains collected compile defects. Early resource admission
// can stop before semantic checks; unreachable-state diagnostic order is not fixed.
type DiagnosticsError struct {
	// Diagnostics contains collected defects; early resource admission can stop collection.
	Diagnostics []Diagnostic
}

// Error returns fixed invalid-definition text without the Diagnostics collection.
func (err *DiagnosticsError) Error() string {
	return "statemachine: invalid definition"
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

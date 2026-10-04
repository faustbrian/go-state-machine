package statemachine

import (
	"os"
	"testing"
)

func TestCompileAggregatePayloadAdmission(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("default aggregate characterization runs only in hosted CI")
	}
	// Every occurrence requires its own immutable payload copy.
	payload := make([]byte, 1<<20)
	effects := make([]Effect, 17)
	for index := range effects {
		effects[index] = Effect{Kind: "notification", Payload: payload}
	}
	definition := Definition[string, string, struct{}]{
		Version: "v1", Initial: "ready",
		States: []StateDefinition[string]{{State: "ready", Entry: effects}},
	}
	machine, err := Compile(definition)
	if machine != nil {
		t.Fatal("definition beyond the default aggregate payload allowance returned a machine")
	}
	assertDiagnostic(t, err, DiagnosticLimitExceeded)
}

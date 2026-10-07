package statemachine

import (
	"os"
	"runtime"
	"testing"
)

func TestCompileAdmissionBeforeEffectCopy(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("allocation characterization runs only in hosted CI")
	}
	const payloadBytes = 8 << 20
	payload := make([]byte, payloadBytes)
	limits := DefaultLimits()
	limits.MaxEffectPayloadBytes = 1 << 10
	definition := Definition[string, string, struct{}]{
		Version: "v1", Initial: "ready",
		States: []StateDefinition[string]{
			{State: "ready", Entry: []Effect{{Kind: "notification", Payload: payload}}},
		},
	}

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	machine, err := CompileWithLimits(definition, limits)
	runtime.ReadMemStats(&after)
	if machine != nil {
		t.Fatal("rejected definition returned a machine")
	}
	assertDiagnostic(t, err, DiagnosticLimitExceeded)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated >= payloadBytes/2 {
		t.Fatalf("rejected effect allocated %d bytes before refusal", allocated)
	}
	runtime.KeepAlive(payload)
}

func TestCompileAdmissionBeforeSourceCopy(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("allocation characterization runs only in hosted CI")
	}
	const sourceCount = 32 << 10
	sources := make([]string, sourceCount)
	limits := DefaultLimits()
	limits.MaxSourcesPerTransition = 1
	definition := Definition[string, string, struct{}]{
		Version: "v1", Initial: "ready",
		States: []StateDefinition[string]{{State: "ready"}},
		Transitions: []TransitionDefinition[string, string, struct{}]{
			{ID: "notification", Sources: sources, Event: "go", To: "ready"},
		},
	}

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	machine, err := CompileWithLimits(definition, limits)
	runtime.ReadMemStats(&after)
	if machine != nil {
		t.Fatal("rejected definition returned a machine")
	}
	assertDiagnostic(t, err, DiagnosticLimitExceeded)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated >= sourceCount*8 {
		t.Fatalf("rejected sources allocated %d bytes before refusal", allocated)
	}
	runtime.KeepAlive(sources)
}

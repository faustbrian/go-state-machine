package statemachine_test

import (
	"context"
	"errors"
	"testing"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

func TestLimitsBoundDefinitionsMetadataAndReplay(t *testing.T) {
	t.Parallel()

	limits := statemachine.Limits{
		MaxStates: 2, MaxTransitions: 2, MaxSourcesPerTransition: 1,
		MaxGuardsPerTransition: 1, MaxEffectsPerPhase: 1,
		MaxEffectPayloadBytes: 4, MaxMetadataBytes: 4, MaxReplayInputs: 1,
	}
	definition := validDefinition()
	machine, err := statemachine.CompileWithLimits(definition, limits)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	_, err = machine.Transition(context.Background(), orderPending, orderPay, 1, statemachine.Metadata{CorrelationID: "12345"})
	if !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("metadata error = %v, want ErrLimitExceeded", err)
	}
	_, err = machine.Replay(context.Background(), []statemachine.Input[orderEvent, int]{
		{Event: orderPay}, {Event: orderPay},
	})
	if !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("replay error = %v, want ErrLimitExceeded", err)
	}

	definition.States = append(definition.States, statemachine.StateDefinition[orderState]{State: "extra"})
	_, err = statemachine.CompileWithLimits(definition, limits)
	var diagnostics *statemachine.DiagnosticsError
	if !errors.As(err, &diagnostics) || !diagnostics.Has(statemachine.DiagnosticLimitExceeded) {
		t.Fatalf("compile error = %v, want limit diagnostic", err)
	}
}

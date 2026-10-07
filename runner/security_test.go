package runner_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/go-state-machine/v2/runner"
)

func TestPublicErrorsDoNotRenderAttackerControlledValues(t *testing.T) {
	t.Parallel()

	sensitive := errors.New("attacker-payload-customer-token")
	effectErr := &runner.EffectError{
		Index: 1, Kind: sensitive.Error(), Outcome: runner.OutcomePermanent, Cause: sensitive,
	}
	if !errors.Is(effectErr, sensitive) || strings.Contains(effectErr.Error(), sensitive.Error()) {
		t.Fatalf("effect error = %q, want redacted wrapped cause", effectErr.Error())
	}
	recorderErr := &runner.RecorderError{Index: 1, Cause: sensitive}
	if !errors.Is(recorderErr, sensitive) || strings.Contains(recorderErr.Error(), sensitive.Error()) {
		t.Fatalf("recorder error = %q, want redacted wrapped cause", recorderErr.Error())
	}
}

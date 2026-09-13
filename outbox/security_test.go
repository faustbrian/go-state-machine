package outbox_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/go-state-machine/v2/outbox"
)

func TestOperationErrorDoesNotRenderAttackerControlledValues(t *testing.T) {
	t.Parallel()

	sensitive := errors.New("attacker-payload-customer-token")
	err := &outbox.OperationError{
		Operation: "publish", MessageID: sensitive.Error(), Cause: sensitive,
	}
	if !errors.Is(err, sensitive) || strings.Contains(err.Error(), sensitive.Error()) {
		t.Fatalf("operation error = %q, want redacted wrapped cause", err.Error())
	}
}

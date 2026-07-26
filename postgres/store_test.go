package postgres_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/state-machine/postgres"
)

func TestNewValidatesDependenciesAndSchema(t *testing.T) {
	t.Parallel()

	_, err := postgres.New[string, string](postgres.Options[string, string]{Schema: "unsafe-name"})
	if !errors.Is(err, postgres.ErrInvalidOptions) {
		t.Fatalf("invalid schema error = %v, want ErrInvalidOptions", err)
	}
	_, err = postgres.New[string, string](postgres.Options[string, string]{Schema: "state_machine"})
	if !errors.Is(err, postgres.ErrInvalidOptions) {
		t.Fatalf("missing dependencies error = %v, want ErrInvalidOptions", err)
	}
}

func TestTextCodecUsesStableUnderlyingValues(t *testing.T) {
	t.Parallel()

	type state string
	codec := postgres.TextCodec[state]()
	encoded, err := codec.Encode(state("awaiting-payment"))
	if err != nil || encoded != "awaiting-payment" {
		t.Fatalf("encode = %q, %v", encoded, err)
	}
	decoded, err := codec.Decode(encoded)
	if err != nil || decoded != state("awaiting-payment") {
		t.Fatalf("decode = %q, %v", decoded, err)
	}
}

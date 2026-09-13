package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	statemachine "github.com/faustbrian/go-state-machine/v2"
	"github.com/faustbrian/go-state-machine/v2/outbox"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNewWithLimitsUsesExplicitMachineLimitsForPersistence(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.Machine.MaxMetadataBytes = 4
	options := Options[string, string]{
		Pool: &pgxpool.Pool{}, Schema: "state_machine",
		StateCodec: TextCodec[string](), EventCodec: TextCodec[string](),
		NewID: func() string { return "id" }, Clock: time.Now,
	}
	store, err := NewWithLimits(options, limits)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := store.validateResult(statemachine.Result[string, string]{
		DefinitionVersion: "v1", TransitionID: "go",
		Metadata: statemachine.Metadata{CorrelationID: "12345"},
	}); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("explicit limit error = %v, want ErrLimitExceeded", err)
	}

	limits.Machine.MaxMetadataBytes = 0
	_, err = NewWithLimits(options, limits)
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("invalid explicit limits error = %v, want ErrInvalidOptions", err)
	}
}

func TestCompareAndTransitionRejectsResultsOutsideMachineLimitsBeforeDatabaseWork(t *testing.T) {
	t.Parallel()

	limits := statemachine.DefaultLimits()
	tests := []struct {
		name   string
		result statemachine.Result[string, string]
	}{
		{
			name: "metadata",
			result: statemachine.Result[string, string]{
				DefinitionVersion: "v1", Previous: "pending", Next: "paid",
				Event: "pay", TransitionID: "pay",
				Metadata: statemachine.Metadata{CorrelationID: strings.Repeat("m", limits.MaxMetadataBytes+1)},
			},
		},
		{
			name: "effect count",
			result: statemachine.Result[string, string]{
				DefinitionVersion: "v1", Previous: "pending", Next: "paid",
				Event: "pay", TransitionID: "pay",
				Effects: make([]statemachine.Effect, limits.MaxEffectsPerPhase*3+1),
			},
		},
		{
			name: "effect payload",
			result: statemachine.Result[string, string]{
				DefinitionVersion: "v1", Previous: "pending", Next: "paid",
				Event: "pay", TransitionID: "pay",
				Effects: []statemachine.Effect{{Kind: "publish", Payload: make([]byte, limits.MaxEffectPayloadBytes+1)}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			began := false
			store := fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) {
				began = true
				return baseTransaction(), nil
			}})
			_, _, err := store.CompareAndTransition(context.Background(), "order-1", 0, test.result, time.Time{})
			if !errors.Is(err, statemachine.ErrLimitExceeded) {
				t.Fatalf("error = %v, want ErrLimitExceeded", err)
			}
			if began {
				t.Fatal("database transaction began for rejected result")
			}
		})
	}
}

func TestPersistenceLimitsRejectEncodedValuesBeforeDatabaseWork(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxInstanceIDBytes = 4
	limits.MaxEncodedStateBytes = 4
	limits.MaxEncodedEventBytes = 4
	limits.MaxIdentifierBytes = 4
	limits.MaxResultBytes = 64
	began := false
	executed := false
	store := fakeStore(fakeDatabase{
		begin: func(context.Context) (transaction, error) {
			began = true
			return baseTransaction(), nil
		},
		exec: func(context.Context, string, ...any) (commandResult, error) {
			executed = true
			return fakeCommandResult(1), nil
		},
	})
	store.limits = limits
	marshaled := false
	store.marshal = func(value any) ([]byte, error) {
		marshaled = true
		return json.Marshal(value)
	}

	if err := store.Create(context.Background(), statemachine.Instance[string]{
		ID: "order", State: "ok", DefinitionVersion: "v1",
	}); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized instance ID error = %v, want ErrLimitExceeded", err)
	}
	if err := store.Create(context.Background(), statemachine.Instance[string]{
		ID: "id", State: "state", DefinitionVersion: "v1",
	}); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized encoded state error = %v, want ErrLimitExceeded", err)
	}

	result := statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: "ok", Next: "done",
		Event: "event", TransitionID: "go",
	}
	if _, _, err := store.CompareAndTransition(context.Background(), "id", 0, result, time.Time{}); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized encoded event error = %v, want ErrLimitExceeded", err)
	}
	result.Event = "go"
	result.TransitionID = "transition"
	if _, _, err := store.CompareAndTransition(context.Background(), "id", 0, result, time.Time{}); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized transition ID error = %v, want ErrLimitExceeded", err)
	}
	result.TransitionID = "go"
	result.Effects = []statemachine.Effect{{Kind: "send", Payload: make([]byte, 48)}}
	if _, _, err := store.CompareAndTransition(context.Background(), "id", 0, result, time.Time{}); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized encoded result error = %v, want ErrLimitExceeded", err)
	}
	if began || executed || marshaled {
		t.Fatalf("expensive work used for rejected input: began=%t executed=%t marshaled=%t", began, executed, marshaled)
	}
}

func TestPostgresErrorsDoNotRenderSensitiveCauses(t *testing.T) {
	t.Parallel()

	sensitive := errors.New("attacker-payload-customer-token")
	store := fakeStore(fakeDatabase{})
	store.stateCodec.Encode = func(string) (string, error) { return "", sensitive }
	err := store.Create(context.Background(), statemachine.Instance[string]{
		ID: "order-1", State: "pending", DefinitionVersion: "v1",
	})
	if !errors.Is(err, sensitive) {
		t.Fatalf("error = %v, want wrapped cause", err)
	}
	if strings.Contains(err.Error(), sensitive.Error()) {
		t.Fatalf("error exposed sensitive cause: %q", err.Error())
	}
}

func TestPersistedOutboxErrorsAreRedacted(t *testing.T) {
	t.Parallel()

	sensitive := errors.New("attacker-payload-customer-token")
	text := boundedErrorText(sensitive)
	if strings.Contains(text, sensitive.Error()) {
		t.Fatalf("persisted error exposed sensitive cause: %q", text)
	}
	if text == "" {
		t.Fatal("persisted error omitted its redaction marker")
	}
}

func TestTransitionRollbackUsesBoundedCleanupContext(t *testing.T) {
	t.Parallel()

	rolledBack := false
	tx := baseTransaction()
	tx.queryRow = func(context.Context, string, ...any) row {
		return fakeRow{scan: func(...any) error { return errors.New("update failed") }}
	}
	tx.rollback = func(ctx context.Context) error {
		rolledBack = true
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 5*time.Second {
			t.Fatalf("rollback context deadline = %v, present=%t", deadline, ok)
		}
		return nil
	}
	store := fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) { return tx, nil }})
	_, _, _ = store.CompareAndTransition(context.Background(), "id", 0, statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: "a", Next: "b", Event: "go", TransitionID: "go",
	}, time.Time{})
	if !rolledBack {
		t.Fatal("transaction was not rolled back")
	}
}

func TestClaimRejectsOversizedPersistedMessagesBeforeLeasing(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.Machine.MaxEffectPayloadBytes = 4
	leased := false
	tx := baseTransaction()
	tx.query = func(context.Context, string, ...any) (rows, error) {
		return &fakeRows{next: []bool{true}, scan: func(destinations ...any) error {
			*destinations[0].(*string) = "id"
			*destinations[1].(*string) = "instance"
			*destinations[2].(*int64) = 1
			*destinations[3].(*int) = 0
			*destinations[4].(*string) = "kind"
			*destinations[5].(*[]byte) = []byte("12345")
			*destinations[6].(*time.Time) = time.Unix(1, 0)
			*destinations[7].(*int) = 0
			return nil
		}}, nil
	}
	tx.exec = func(context.Context, string, ...any) (commandResult, error) {
		leased = true
		return fakeCommandResult(1), nil
	}
	store := fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) { return tx, nil }})
	store.limits = limits
	_, err := store.Claim(context.Background(), outbox.ClaimRequest{
		Owner: "worker", Limit: 1, LeaseDuration: time.Second,
	})
	if !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("claim error = %v, want ErrLimitExceeded", err)
	}
	if leased {
		t.Fatal("oversized message was leased")
	}
}

func TestDecodeResultRejectsPersistedMachineLimitBypass(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.Machine.MaxMetadataBytes = 4
	store := fakeStore(fakeDatabase{})
	store.limits = limits
	_, err := store.decodeResult([]byte(`{
		"definition_version":"v1","previous":"a","next":"b",
		"event":"go","transition_id":"go",
		"metadata":{"CorrelationID":"12345","CausationID":""},"effects":[]
	}`))
	if !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("decode error = %v, want ErrLimitExceeded", err)
	}
}

func TestHistoryAndClaimBoundAggregateReturnedBytes(t *testing.T) {
	t.Parallel()

	encoded := []byte(`{"definition_version":"v1","previous":"a","next":"b","event":"go","transition_id":"go","metadata":{},"effects":[]}`)
	limits := DefaultLimits()
	limits.MaxHistoryBytes = len(encoded) + 1
	limits.MaxClaimBytes = 5

	historyRows := &fakeRows{next: []bool{true, true}, scan: func(destinations ...any) error {
		*destinations[0].(*int64) = 1
		*destinations[1].(*[]byte) = append([]byte(nil), encoded...)
		*destinations[2].(*time.Time) = time.Unix(1, 0)
		return nil
	}}
	store := fakeStore(fakeDatabase{query: func(context.Context, string, ...any) (rows, error) {
		return historyRows, nil
	}})
	store.limits = limits
	if _, err := store.History(context.Background(), "id", 0, 2); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("history error = %v, want ErrLimitExceeded", err)
	}

	claimRows := &fakeRows{next: []bool{true, true}, scan: func(destinations ...any) error {
		*destinations[0].(*string) = "id"
		*destinations[1].(*string) = "instance"
		*destinations[2].(*int64) = 1
		*destinations[3].(*int) = 0
		*destinations[4].(*string) = "kind"
		*destinations[5].(*[]byte) = []byte("123")
		*destinations[6].(*time.Time) = time.Unix(1, 0)
		*destinations[7].(*int) = 0
		return nil
	}}
	tx := baseTransaction()
	tx.query = func(context.Context, string, ...any) (rows, error) { return claimRows, nil }
	store = fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) { return tx, nil }})
	store.limits = limits
	if _, err := store.Claim(context.Background(), outbox.ClaimRequest{
		Owner: "worker", Limit: 2, LeaseDuration: time.Second,
	}); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("claim error = %v, want ErrLimitExceeded", err)
	}
}

func TestPersistenceBoundaryInputsAreRejectedBeforeDatabaseWork(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxInstanceIDBytes = 3
	limits.MaxIdentifierBytes = 3
	limits.MaxEncodedStateBytes = 3
	store := fakeStore(fakeDatabase{})
	store.limits = limits

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	validResult := statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: "a", Next: "b", Event: "go", TransitionID: "go",
	}
	if _, err := store.Load(canceled, "id"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Load error = %v", err)
	}
	if _, err := store.Load(context.Background(), "long"); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized Load identity error = %v", err)
	}
	if _, _, err := store.CompareAndTransition(canceled, "id", 0, validResult, time.Time{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled transition error = %v", err)
	}
	if _, _, err := store.CompareAndTransition(context.Background(), "", 0, validResult, time.Time{}); !errors.Is(err, statemachine.ErrInvalidStoreInput) {
		t.Fatalf("empty transition identity error = %v", err)
	}
	if _, _, err := store.CompareAndTransition(context.Background(), "long", 0, validResult, time.Time{}); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized transition identity error = %v", err)
	}
	if _, err := store.History(canceled, "id", 0, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled History error = %v", err)
	}
	if _, err := store.History(context.Background(), "long", 0, 1); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized History identity error = %v", err)
	}
	if err := store.SaveSnapshot(canceled, statemachine.Snapshot[string]{InstanceID: "id", DefinitionVersion: "v1"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled SaveSnapshot error = %v", err)
	}
	if err := store.SaveSnapshot(context.Background(), statemachine.Snapshot[string]{}); !errors.Is(err, statemachine.ErrInvalidStoreInput) {
		t.Fatalf("missing snapshot identity error = %v", err)
	}
	if err := store.SaveSnapshot(context.Background(), statemachine.Snapshot[string]{InstanceID: "long", DefinitionVersion: "v1"}); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized snapshot identity error = %v", err)
	}
	if err := store.SaveSnapshot(context.Background(), statemachine.Snapshot[string]{InstanceID: "id", State: "long", DefinitionVersion: "v1"}); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized snapshot state error = %v", err)
	}
	if _, err := store.LoadSnapshot(canceled, "id"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled LoadSnapshot error = %v", err)
	}
	if _, err := store.LoadSnapshot(context.Background(), "long"); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized LoadSnapshot identity error = %v", err)
	}
}

func TestPersistenceRejectsOversizedLoadedRows(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxEncodedStateBytes = 3
	limits.MaxIdentifierBytes = 3
	database := fakeDatabase{queryRow: func(context.Context, string, ...any) row {
		return fakeRow{scan: func(destinations ...any) error {
			*destinations[0].(*string) = "long"
			*destinations[1].(*string) = "v1"
			return nil
		}}
	}}
	store := fakeStore(database)
	store.limits = limits
	if _, err := store.Load(context.Background(), "id"); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized loaded instance error = %v", err)
	}
	if _, err := store.LoadSnapshot(context.Background(), "id"); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized loaded snapshot error = %v", err)
	}
}

func TestResultValidationRejectsMalformedEffectIdentity(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxIdentifierBytes = 3
	store := fakeStore(fakeDatabase{})
	store.limits = limits
	base := statemachine.Result[string, string]{DefinitionVersion: "v1", TransitionID: "go"}

	missing := base
	missing.Effects = []statemachine.Effect{{}}
	if err := store.validateResult(missing); !errors.Is(err, statemachine.ErrInvalidStoreInput) {
		t.Fatalf("missing effect kind error = %v", err)
	}
	oversized := base
	oversized.Effects = []statemachine.Effect{{Kind: "long"}}
	if err := store.validateResult(oversized); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized effect kind error = %v", err)
	}
}

func TestEncodedResultBoundsCoverStoredAndAggregateRepresentations(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxEncodedStateBytes = 3
	limits.MaxEncodedEventBytes = 3
	limits.MaxIdentifierBytes = 3
	limits.MaxResultBytes = 1_000
	store := fakeStore(fakeDatabase{})
	store.limits = limits
	base := statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: "a", Next: "b", Event: "go", TransitionID: "go",
	}

	state := base
	state.Next = "long"
	if _, _, err := store.encodeResult(state); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized encoded state error = %v", err)
	}
	event := base
	event.Event = "long"
	if _, _, err := store.encodeResult(event); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized encoded event error = %v", err)
	}
	store.marshal = func(any) ([]byte, error) { return make([]byte, limits.MaxResultBytes+1), nil }
	if _, _, err := store.encodeResult(base); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized marshaled result error = %v", err)
	}
	if _, err := store.decodeResult(make([]byte, limits.MaxResultBytes+1)); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized stored result error = %v", err)
	}
	if _, err := store.decodeResult([]byte(`{"definition_version":"v1","previous":"long","next":"b","event":"go","transition_id":"go"}`)); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized decoded result field error = %v", err)
	}
	if resultDocumentFits(resultDocument{Effects: []statemachine.Effect{{Kind: "kind", Payload: []byte{1}}}}, 327) {
		t.Fatal("aggregate preflight accepted an oversized effect document")
	}
	if !resultDocumentFits(resultDocument{Effects: []statemachine.Effect{{Kind: "kind", Payload: []byte{1}}}}, 400) {
		t.Fatal("aggregate preflight rejected a bounded non-multiple-of-three payload")
	}
}

func TestTransactionsReportBeginFailuresWithoutExposingTheCause(t *testing.T) {
	t.Parallel()

	sensitive := errors.New("attacker-controlled-driver-detail")
	store := fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) { return nil, sensitive }})
	_, _, err := store.CompareAndTransition(context.Background(), "id", 0, statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: "a", Next: "b", Event: "go", TransitionID: "go",
	}, time.Time{})
	if !errors.Is(err, sensitive) || strings.Contains(err.Error(), sensitive.Error()) {
		t.Fatalf("transition begin error = %q, unwraps cause=%t", err, errors.Is(err, sensitive))
	}
	_, err = store.Claim(context.Background(), outbox.ClaimRequest{Owner: "owner", Limit: 1, LeaseDuration: time.Second})
	if !errors.Is(err, sensitive) || strings.Contains(err.Error(), sensitive.Error()) {
		t.Fatalf("claim begin error = %q, unwraps cause=%t", err, errors.Is(err, sensitive))
	}
}

func TestTransitionRejectsOversizedGeneratedOutboxIdentity(t *testing.T) {
	t.Parallel()

	tx := baseTransaction()
	tx.queryRow = lockingRow
	limits := DefaultLimits()
	limits.MaxIdentifierBytes = 3
	store := fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) { return tx, nil }})
	store.limits = limits
	store.newID = func() string { return "long" }
	_, _, err := store.CompareAndTransition(context.Background(), "id", 0, statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: "a", Next: "b", Event: "go", TransitionID: "go",
		Effects: []statemachine.Effect{{Kind: "job"}},
	}, time.Time{})
	if !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized outbox ID error = %v", err)
	}
}

func TestOutboxRejectsMalformedOrOversizedLeaseData(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxIdentifierBytes = 3
	store := fakeStore(fakeDatabase{})
	store.limits = limits
	request := outbox.ClaimRequest{Owner: "long", Limit: 1, LeaseDuration: time.Second}
	if _, err := store.Claim(context.Background(), request); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized owner error = %v", err)
	}
	if err := store.MarkPublished(context.Background(), outbox.LeaseRef{ID: "long", Token: "tok"}, time.Time{}); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized lease reference error = %v", err)
	}

	malformedTx := baseTransaction()
	malformedTx.query = func(context.Context, string, ...any) (rows, error) {
		return &fakeRows{next: []bool{true}, scan: func(...any) error { return nil }}, nil
	}
	store = fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) { return malformedTx, nil }})
	if _, err := store.Claim(context.Background(), outbox.ClaimRequest{Owner: "own", Limit: 1, LeaseDuration: time.Second}); !errors.Is(err, statemachine.ErrInvalidStoreInput) {
		t.Fatalf("malformed claimed row error = %v", err)
	}

	tokenTx := baseTransaction()
	tokenTx.query = func(context.Context, string, ...any) (rows, error) {
		return &fakeRows{next: []bool{true}, scan: func(destinations ...any) error {
			*destinations[0].(*string) = "id"
			*destinations[1].(*string) = "in"
			*destinations[2].(*int64) = 1
			*destinations[3].(*int) = 0
			*destinations[4].(*string) = "job"
			*destinations[6].(*time.Time) = time.Unix(1, 0)
			return nil
		}}, nil
	}
	store = fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) { return tokenTx, nil }})
	store.limits = limits
	store.newID = func() string { return "long" }
	if _, err := store.Claim(context.Background(), outbox.ClaimRequest{Owner: "own", Limit: 1, LeaseDuration: time.Second}); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("oversized lease token error = %v", err)
	}
}

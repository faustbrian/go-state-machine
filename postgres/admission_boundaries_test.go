package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	statemachine "github.com/faustbrian/go-state-machine/v2"
	"github.com/faustbrian/go-state-machine/v2/outbox"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExplicitPersistenceLimitsRequirePositiveFields(t *testing.T) {
	options := Options[string, string]{
		Pool: &pgxpool.Pool{}, Schema: "state_machine",
		StateCodec: TextCodec[string](), EventCodec: TextCodec[string](),
		NewID: func() string { return "id" }, Clock: time.Now,
	}
	fields := []struct {
		name  string
		field func(*Limits) *int
	}{
		{"states", func(l *Limits) *int { return &l.Machine.MaxStates }},
		{"transitions", func(l *Limits) *int { return &l.Machine.MaxTransitions }},
		{"sources", func(l *Limits) *int { return &l.Machine.MaxSourcesPerTransition }},
		{"guards", func(l *Limits) *int { return &l.Machine.MaxGuardsPerTransition }},
		{"effects", func(l *Limits) *int { return &l.Machine.MaxEffectsPerPhase }},
		{"payload", func(l *Limits) *int { return &l.Machine.MaxEffectPayloadBytes }},
		{"metadata", func(l *Limits) *int { return &l.Machine.MaxMetadataBytes }},
		{"replay", func(l *Limits) *int { return &l.Machine.MaxReplayInputs }},
		{"instance", func(l *Limits) *int { return &l.MaxInstanceIDBytes }},
		{"state", func(l *Limits) *int { return &l.MaxEncodedStateBytes }},
		{"event", func(l *Limits) *int { return &l.MaxEncodedEventBytes }},
		{"identifier", func(l *Limits) *int { return &l.MaxIdentifierBytes }},
		{"result", func(l *Limits) *int { return &l.MaxResultBytes }},
		{"history", func(l *Limits) *int { return &l.MaxHistoryBytes }},
		{"claim", func(l *Limits) *int { return &l.MaxClaimBytes }},
	}
	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			for _, value := range []int{-1, 0, 1} {
				limits := DefaultLimits()
				*field.field(&limits) = value
				store, err := NewWithLimits(options, limits)
				if value <= 0 {
					if store != nil || !errors.Is(err, ErrInvalidOptions) {
						t.Errorf("value %d: invalid bound admitted: store=%v err=%v", value, store != nil, err)
					}
				} else if store == nil || err != nil {
					t.Errorf("positive bound refused: store=%v err=%v", store != nil, err)
				}
			}
		})
	}
}

func TestTransitionAdmissionIndependentInclusiveBoundaries(t *testing.T) {
	cases := []struct {
		name   string
		change func(*statemachine.Result[string, string])
		want   error
	}{
		{"missing version", func(r *statemachine.Result[string, string]) { r.DefinitionVersion = "" }, statemachine.ErrInvalidStoreInput},
		{"missing transition", func(r *statemachine.Result[string, string]) { r.TransitionID = "" }, statemachine.ErrInvalidStoreInput},
		{"version exact", func(r *statemachine.Result[string, string]) { r.DefinitionVersion = "123" }, nil},
		{"version over", func(r *statemachine.Result[string, string]) { r.DefinitionVersion = "1234" }, statemachine.ErrLimitExceeded},
		{"transition exact", func(r *statemachine.Result[string, string]) { r.TransitionID = "123" }, nil},
		{"transition over", func(r *statemachine.Result[string, string]) { r.TransitionID = "1234" }, statemachine.ErrLimitExceeded},
		{"correlation exact", func(r *statemachine.Result[string, string]) { r.Metadata.CorrelationID = "123" }, nil},
		{"correlation over", func(r *statemachine.Result[string, string]) { r.Metadata.CorrelationID = "1234" }, statemachine.ErrLimitExceeded},
		{"causation exact", func(r *statemachine.Result[string, string]) { r.Metadata.CausationID = "123" }, nil},
		{"causation over", func(r *statemachine.Result[string, string]) { r.Metadata.CausationID = "1234" }, statemachine.ErrLimitExceeded},
		{"combined exact", func(r *statemachine.Result[string, string]) {
			r.Metadata.CorrelationID, r.Metadata.CausationID = "1", "23"
		}, nil},
		{"combined over", func(r *statemachine.Result[string, string]) {
			r.Metadata.CorrelationID, r.Metadata.CausationID = "12", "23"
		}, statemachine.ErrLimitExceeded},
		{"effects exact", func(r *statemachine.Result[string, string]) {
			r.Effects = []statemachine.Effect{{Kind: "a"}, {Kind: "b"}, {Kind: "c"}}
		}, nil},
		{"effects over", func(r *statemachine.Result[string, string]) {
			r.Effects = []statemachine.Effect{{Kind: "a"}, {Kind: "b"}, {Kind: "c"}, {Kind: "d"}}
		}, statemachine.ErrLimitExceeded},
		{"missing kind", func(r *statemachine.Result[string, string]) { r.Effects = []statemachine.Effect{{}} }, statemachine.ErrInvalidStoreInput},
		{"kind exact", func(r *statemachine.Result[string, string]) { r.Effects = []statemachine.Effect{{Kind: "123"}} }, nil},
		{"kind over", func(r *statemachine.Result[string, string]) { r.Effects = []statemachine.Effect{{Kind: "1234"}} }, statemachine.ErrLimitExceeded},
		{"payload exact", func(r *statemachine.Result[string, string]) {
			r.Effects = []statemachine.Effect{{Kind: "a", Payload: []byte("123")}}
		}, nil},
		{"payload over", func(r *statemachine.Result[string, string]) {
			r.Effects = []statemachine.Effect{{Kind: "a", Payload: []byte("1234")}}
		}, statemachine.ErrLimitExceeded},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			limits.MaxIdentifierBytes = 3
			limits.Machine.MaxMetadataBytes = 3
			limits.Machine.MaxEffectsPerPhase = 1
			limits.Machine.MaxEffectPayloadBytes = 3
			result := statemachine.Result[string, string]{DefinitionVersion: "v1", TransitionID: "go", Previous: "a", Next: "b", Event: "go"}
			test.change(&result)
			began, committed := false, false
			persistedEffects := 0
			tx := baseTransaction()
			tx.queryRow = lockingRow
			tx.commit = func(context.Context) error { committed = true; return nil }
			tx.exec = func(_ context.Context, query string, values ...any) (commandResult, error) {
				if strings.Contains(query, "state_machine_history") {
					var saved resultDocument
					if err := json.Unmarshal([]byte(values[2].(string)), &saved); err != nil || saved.DefinitionVersion != string(result.DefinitionVersion) || saved.TransitionID != string(result.TransitionID) || saved.Metadata != result.Metadata || len(saved.Effects) != len(result.Effects) {
						t.Error("persisted history differs from admitted input")
					}
				}
				if strings.Contains(query, "state_machine_outbox") {
					effect := result.Effects[persistedEffects]
					if values[4] != effect.Kind || string(values[5].([]byte)) != string(effect.Payload) {
						t.Error("persisted effect differs from admitted input")
					}
					persistedEffects++
				}
				return fakeCommandResult(1), nil
			}
			store := fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) { began = true; return tx, nil }})
			store.limits = limits
			store.newID = func() string { return "e" + strconv.Itoa(persistedEffects) }
			instance, history, err := store.CompareAndTransition(t.Context(), "one", 0, result, time.Unix(1, 0))
			if test.want != nil {
				if !errors.Is(err, test.want) || began || committed || instance.ID != "" || history.InstanceID != "" {
					t.Fatalf("rejected result crossed acquisition/publication: err=%v began=%v committed=%v", err, began, committed)
				}
			} else if err != nil || !began || !committed || instance.State != "b" || instance.DefinitionVersion != result.DefinitionVersion || instance.LockVersion != 1 || history.Sequence != 1 || history.Result.TransitionID != result.TransitionID || history.Result.Metadata != result.Metadata || persistedEffects != len(result.Effects) {
				t.Fatalf("inclusive admitted result not persisted intact: err=%v began=%v committed=%v effects=%d", err, began, committed, persistedEffects)
			}
		})
	}
}

func TestClaimCandidateIndependentAdmission(t *testing.T) {
	cases := []struct {
		name                        string
		id, instance, kind, payload string
		index, attempts             int
		want                        error
	}{
		{"valid exact", "123", "456", "789", "abc", 0, 0, nil},
		{"missing id", "", "456", "789", "abc", 0, 0, statemachine.ErrInvalidStoreInput},
		{"missing instance", "123", "", "789", "abc", 0, 0, statemachine.ErrInvalidStoreInput},
		{"missing kind", "123", "456", "", "abc", 0, 0, statemachine.ErrInvalidStoreInput},
		{"negative index", "123", "456", "789", "abc", -1, 0, statemachine.ErrInvalidStoreInput},
		{"negative attempts", "123", "456", "789", "abc", 0, -1, statemachine.ErrInvalidStoreInput},
		{"id over", "1234", "456", "789", "abc", 0, 0, statemachine.ErrLimitExceeded},
		{"instance over", "123", "4567", "789", "abc", 0, 0, statemachine.ErrLimitExceeded},
		{"kind over", "123", "456", "7890", "abc", 0, 0, statemachine.ErrLimitExceeded},
		{"payload over", "123", "456", "789", "abcd", 0, 0, statemachine.ErrLimitExceeded},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			limits.MaxIdentifierBytes, limits.MaxInstanceIDBytes, limits.Machine.MaxEffectPayloadBytes = 3, 3, 3
			selected := &fakeRows{next: []bool{true}, scan: func(dest ...any) error {
				*dest[0].(*string), *dest[1].(*string), *dest[2].(*int64) = test.id, test.instance, 1
				*dest[3].(*int), *dest[4].(*string), *dest[5].(*[]byte) = test.index, test.kind, []byte(test.payload)
				*dest[6].(*time.Time), *dest[7].(*int) = time.Unix(1, 0), test.attempts
				return nil
			}}
			updated, committed, rolledBack := false, false, false
			tx := baseTransaction()
			tx.query = func(context.Context, string, ...any) (rows, error) { return selected, nil }
			tx.exec = func(context.Context, string, ...any) (commandResult, error) {
				updated = true
				return fakeCommandResult(1), nil
			}
			tx.commit = func(context.Context) error { committed = true; return nil }
			tx.rollback = func(context.Context) error { rolledBack = true; return nil }
			store := fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) { return tx, nil }})
			store.limits = limits
			claims, err := store.Claim(t.Context(), outbox.ClaimRequest{Owner: "own", Limit: 1, LeaseDuration: time.Second})
			if !selected.closed || !rolledBack {
				t.Fatal("claim did not release rows/transaction")
			}
			if test.want != nil {
				if !errors.Is(err, test.want) || claims != nil || updated || committed {
					t.Fatalf("invalid candidate crossed lease/publication: err=%v updated=%v committed=%v", err, updated, committed)
				}
			} else if err != nil || len(claims) != 1 || !updated || !committed || claims[0].Message.ID != test.id || string(claims[0].Message.InstanceID) != test.instance || claims[0].Message.Sequence != 1 || claims[0].Message.Effect.Kind != test.kind || string(claims[0].Message.Effect.Payload) != test.payload || claims[0].Message.Attempts != 1 || claims[0].Token != "id" {
				t.Fatalf("exact candidate not leased intact: err=%v updated=%v committed=%v", err, updated, committed)
			}
		})
	}
}

func TestClaimAggregateRejectsBeforeAnyLeaseUpdate(t *testing.T) {
	cases := []struct {
		name          string
		budget        int
		invalidSecond bool
		want          error
	}{
		{"aggregate over", 7, false, statemachine.ErrLimitExceeded},
		{"aggregate exact", 8, false, nil},
		{"invalid second candidate", 8, true, statemachine.ErrInvalidStoreInput},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			selected := &fakeRows{next: []bool{true, true}}
			selected.scan = func(dest ...any) error {
				*dest[0].(*string), *dest[1].(*string), *dest[2].(*int64) = []string{"a", "b"}[selected.index-1], "i", 1
				*dest[4].(*string), *dest[5].(*[]byte) = "k", []byte("p")
				if test.invalidSecond && selected.index == 2 {
					*dest[0].(*string) = ""
				}
				return nil
			}
			updated, committed, rolledBack := 0, false, false
			tx := baseTransaction()
			tx.query = func(context.Context, string, ...any) (rows, error) { return selected, nil }
			tx.exec = func(context.Context, string, ...any) (commandResult, error) {
				updated++
				return fakeCommandResult(1), nil
			}
			tx.commit = func(context.Context) error { committed = true; return nil }
			tx.rollback = func(context.Context) error { rolledBack = true; return nil }
			store := fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) { return tx, nil }})
			limits := DefaultLimits()
			limits.MaxClaimBytes = test.budget
			store.limits = limits
			claims, err := store.Claim(t.Context(), outbox.ClaimRequest{Owner: "own", Limit: 2, LeaseDuration: time.Second})
			if !selected.closed || !rolledBack {
				t.Fatal("aggregate claim leaked resources")
			}
			if test.want != nil {
				if !errors.Is(err, test.want) || claims != nil || updated != 0 || committed {
					t.Fatalf("aggregate overrun published or leased: err=%v updates=%d commit=%v", err, updated, committed)
				}
			} else if err != nil || len(claims) != 2 || updated != 2 || !committed || claims[0].Message.ID != "a" || claims[1].Message.ID != "b" {
				t.Fatalf("exact aggregate not admitted: err=%v updates=%d commit=%v", err, updated, committed)
			}
		})
	}
}

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	statemachine "github.com/faustbrian/go-state-machine/v2"
	"github.com/faustbrian/go-state-machine/v2/outbox"
)

// These public adapter tests isolate inclusive limits from earlier admission
// guards. Accepted values must survive persistence/publication unchanged;
// rejected values must not publish, commit, or leak acquired resources.
func TestTransitionGeneratedAndInstanceIdentityBoundaries(t *testing.T) {
	for _, field := range []string{"instance", "outbox"} {
		for _, size := range []int{3, 4} {
			t.Run(field+"/"+strconv.Itoa(size), func(t *testing.T) {
				id, generated := "i", "o"
				if field == "instance" {
					id = strings.Repeat("i", size)
				} else {
					generated = strings.Repeat("o", size)
				}
				result := statemachine.Result[string, string]{DefinitionVersion: "v", TransitionID: "t", Previous: "a", Next: "b", Event: "e", Effects: []statemachine.Effect{{Kind: "k", Payload: []byte("p")}}}
				began, committed, rolledBack, inserted := false, false, false, false
				tx := baseTransaction()
				tx.queryRow = lockingRow
				tx.commit = func(context.Context) error { committed = true; return nil }
				tx.rollback = func(context.Context) error { rolledBack = true; return nil }
				tx.exec = func(_ context.Context, query string, args ...any) (commandResult, error) {
					if strings.Contains(query, "state_machine_outbox") {
						inserted = true
						if args[0] != generated || args[1] != statemachine.InstanceID(id) || args[4] != "k" || string(args[5].([]byte)) != "p" {
							t.Fatal("outbox values changed")
						}
					}
					return fakeCommandResult(1), nil
				}
				store := fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) { began = true; return tx, nil }})
				store.limits = DefaultLimits()
				store.limits.MaxInstanceIDBytes, store.limits.MaxIdentifierBytes = 3, 3
				store.newID = func() string { return generated }
				instance, history, err := store.CompareAndTransition(t.Context(), statemachine.InstanceID(id), 0, result, time.Unix(1, 0))
				if size == 4 {
					if !errors.Is(err, statemachine.ErrLimitExceeded) || committed || inserted || instance.ID != "" || history.InstanceID != "" || began != (field == "outbox") {
						t.Fatalf("over-cap identity crossed boundary: %v", err)
					}
				} else if err != nil || !committed || !inserted || instance.ID != statemachine.InstanceID(id) || history.InstanceID != statemachine.InstanceID(id) || !reflect.DeepEqual(history.Result, result) {
					t.Fatalf("exact identity refused or changed: %v", err)
				}
				if rolledBack != began {
					t.Fatal("transaction cleanup missing")
				}
			})
		}
	}
}

func TestGeneratedClaimTokenInclusiveBoundary(t *testing.T) {
	for _, size := range []int{3, 4} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			now := time.Unix(10, 0)
			selected := &fakeRows{next: []bool{true}, scan: func(dest ...any) error {
				*dest[0].(*string), *dest[1].(*string), *dest[2].(*int64) = "m", "i", 2
				*dest[3].(*int), *dest[4].(*string), *dest[5].(*[]byte) = 1, "k", []byte("p")
				*dest[6].(*time.Time), *dest[7].(*int) = now, 3
				return nil
			}}
			updated, committed, rolledBack := false, false, false
			token := strings.Repeat("x", size)
			tx := baseTransaction()
			tx.query = func(context.Context, string, ...any) (rows, error) { return selected, nil }
			tx.exec = func(_ context.Context, _ string, args ...any) (commandResult, error) {
				updated = true
				if !reflect.DeepEqual(args, []any{"own", token, now.Add(time.Second), "m"}) {
					t.Fatal("lease values changed")
				}
				return fakeCommandResult(1), nil
			}
			tx.commit = func(context.Context) error { committed = true; return nil }
			tx.rollback = func(context.Context) error { rolledBack = true; return nil }
			store := fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) { return tx, nil }})
			store.limits = DefaultLimits()
			store.limits.MaxIdentifierBytes = 3
			store.newID = func() string { return token }
			store.clock = func() time.Time { return now }
			claims, err := store.Claim(t.Context(), outbox.ClaimRequest{Owner: "own", Limit: 1, LeaseDuration: time.Second})
			if !selected.closed || !rolledBack {
				t.Fatal("claim resources leaked")
			}
			if size == 4 {
				if !errors.Is(err, statemachine.ErrLimitExceeded) || claims != nil || updated || committed {
					t.Fatalf("over-cap token leased/published: %v", err)
				}
			} else if err != nil || !updated || !committed || len(claims) != 1 || claims[0].Token != token || claims[0].Message.ID != "m" || claims[0].Message.InstanceID != "i" || claims[0].Message.Sequence != 2 || claims[0].Message.Index != 1 || claims[0].Message.Effect.Kind != "k" || string(claims[0].Message.Effect.Payload) != "p" || claims[0].Message.Attempts != 4 || !claims[0].Message.OccurredAt.Equal(now) || !claims[0].LeasedUntil.Equal(now.Add(time.Second)) {
				t.Fatalf("exact token claim changed: %v", err)
			}
		})
	}
}

func TestFinishLeaseIndependentInclusiveBoundaries(t *testing.T) {
	for _, operation := range []string{"published", "retry", "dead"} {
		for _, field := range []string{"id", "token"} {
			for _, size := range []int{3, 4} {
				t.Run(operation+"/"+field+"/"+strconv.Itoa(size), func(t *testing.T) {
					ref := outbox.LeaseRef{ID: "i", Token: "t"}
					if field == "id" {
						ref.ID = strings.Repeat("i", size)
					} else {
						ref.Token = strings.Repeat("t", size)
					}
					at, executed := time.Unix(7, 0), false
					store := fakeStore(fakeDatabase{exec: func(_ context.Context, query string, args ...any) (commandResult, error) {
						executed = true
						assignment, cause := "published_at", ""
						if operation == "retry" {
							assignment, cause = "available_at", "redacted"
						}
						if operation == "dead" {
							assignment, cause = "dead_lettered_at", "redacted"
						}
						if !strings.Contains(query, assignment+" = $3") || !reflect.DeepEqual(args, []any{ref.ID, ref.Token, at, cause}) {
							t.Fatal("finish lease values changed")
						}
						return fakeCommandResult(1), nil
					}})
					store.limits = DefaultLimits()
					store.limits.MaxIdentifierBytes = 3
					var err error
					switch operation {
					case "published":
						err = store.MarkPublished(t.Context(), ref, at)
					case "retry":
						err = store.Retry(t.Context(), ref, at, errors.New("private"))
					case "dead":
						err = store.DeadLetter(t.Context(), ref, at, errors.New("private"))
					}
					if size == 4 {
						if !errors.Is(err, statemachine.ErrLimitExceeded) || executed {
							t.Fatalf("over-cap ref executed: %v", err)
						}
					} else if err != nil || !executed {
						t.Fatalf("exact ref refused: %v", err)
					}
				})
			}
		}
	}
}

func TestHistoryIndependentInclusiveBoundaries(t *testing.T) {
	for _, field := range []string{"instance", "document", "previous", "next", "event", "version", "transition"} {
		for _, over := range []bool{false, true} {
			t.Run(field+"/"+strconv.FormatBool(over), func(t *testing.T) {
				size := 3
				if over {
					size++
				}
				id := "i"
				doc := resultDocument{DefinitionVersion: "v", TransitionID: "t", Previous: "a", Next: "b", Event: "e"}
				value := strings.Repeat("x", size)
				switch field {
				case "instance":
					id = value
				case "previous":
					doc.Previous = value
				case "next":
					doc.Next = value
				case "event":
					doc.Event = value
				case "version":
					doc.DefinitionVersion = value
				case "transition":
					doc.TransitionID = value
				}
				encoded, err := json.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				at := time.Unix(8, 0)
				selected := &fakeRows{next: []bool{true}, scan: func(dest ...any) error {
					*dest[0].(*int64), *dest[1].(*[]byte), *dest[2].(*time.Time) = 1, encoded, at
					return nil
				}}
				queried, decoded := false, false
				store := fakeStore(fakeDatabase{query: func(_ context.Context, _ string, args ...any) (rows, error) {
					queried = true
					if !reflect.DeepEqual(args, []any{statemachine.InstanceID(id), uint64(0), 1}) {
						t.Fatal("history lookup changed")
					}
					return selected, nil
				}})
				store.limits = DefaultLimits()
				store.limits.MaxInstanceIDBytes, store.limits.MaxEncodedStateBytes, store.limits.MaxEncodedEventBytes, store.limits.MaxIdentifierBytes = 3, 3, 3, 3
				if field == "document" {
					store.limits.MaxResultBytes = len(encoded)
					if over {
						store.limits.MaxResultBytes--
					}
				}
				store.stateCodec.Decode = func(s string) (string, error) { decoded = true; return s, nil }
				entries, err := store.History(t.Context(), statemachine.InstanceID(id), 0, 1)
				if over {
					if !errors.Is(err, statemachine.ErrLimitExceeded) || entries != nil || decoded || queried != (field != "instance") {
						t.Fatalf("over-cap history decoded/published: %v", err)
					}
				} else if err != nil || len(entries) != 1 || entries[0].InstanceID != statemachine.InstanceID(id) || entries[0].Sequence != 1 || !entries[0].OccurredAt.Equal(at) || entries[0].Result.Previous != doc.Previous || entries[0].Result.Next != doc.Next || entries[0].Result.Event != doc.Event || string(entries[0].Result.DefinitionVersion) != doc.DefinitionVersion || string(entries[0].Result.TransitionID) != doc.TransitionID {
					t.Fatalf("exact history changed/refused: %v", err)
				}
				if selected.closed != queried {
					t.Fatal("history rows leaked")
				}
			})
		}
	}
}

func TestTransitionAggregatePreflightDebitsAndPayloadRounding(t *testing.T) {
	// Each prefix budget is one byte short at a different first debit. All
	// individual field limits remain generous, so only aggregate admission acts.
	result := statemachine.Result[string, string]{DefinitionVersion: "vv", Previous: "aaa", Next: "bbbb", Event: "eeeee", TransitionID: "tttttt", Metadata: statemachine.Metadata{CorrelationID: "ccccccc", CausationID: "dddddddd"}, Effects: []statemachine.Effect{{Kind: "kkkkkkkkk", Payload: []byte("p")}}}
	debits := []int{256, 12, 18, 24, 30, 36, 42, 48, 48, 54, 4}
	prefix := 0
	for index, debit := range debits {
		prefix += debit
		t.Run("first-debit-"+strconv.Itoa(index), func(t *testing.T) { assertTransitionAggregateBudget(t, result, prefix-1, false) })
	}
	for payload := 0; payload <= 4; payload++ {
		result := result
		result.Effects = []statemachine.Effect{{Kind: "kkkkkkkkk", Payload: []byte(strings.Repeat("p", payload))}}
		if payload == 0 {
			result.Effects[0].Payload = nil
		}
		budget := 256 + 6*(2+3+4+5+6+7+8) + 48 + 6*9 + 4*((payload+2)/3)
		for _, exact := range []bool{false, true} {
			t.Run("payload-"+strconv.Itoa(payload)+"/"+strconv.FormatBool(exact), func(t *testing.T) {
				n := budget
				if !exact {
					n--
				}
				assertTransitionAggregateBudget(t, result, n, exact)
			})
		}
	}
}

func assertTransitionAggregateBudget(t *testing.T, result statemachine.Result[string, string], budget int, accepted bool) {
	t.Helper()
	began, committed, rolledBack := false, false, false
	persisted := 0
	tx := baseTransaction()
	tx.queryRow = lockingRow
	tx.commit = func(context.Context) error { committed = true; return nil }
	tx.rollback = func(context.Context) error { rolledBack = true; return nil }
	tx.exec = func(_ context.Context, query string, args ...any) (commandResult, error) {
		if strings.Contains(query, "state_machine_history") {
			var doc resultDocument
			if err := json.Unmarshal([]byte(args[2].(string)), &doc); err != nil || doc.Previous != result.Previous || doc.Next != result.Next || doc.Event != result.Event || doc.DefinitionVersion != string(result.DefinitionVersion) || doc.TransitionID != string(result.TransitionID) || doc.Metadata != result.Metadata {
				t.Fatal("aggregate history changed")
			}
		}
		if strings.Contains(query, "state_machine_outbox") {
			persisted++
			if args[4] != result.Effects[0].Kind || string(args[5].([]byte)) != string(result.Effects[0].Payload) {
				t.Fatal("aggregate effect changed")
			}
		}
		return fakeCommandResult(1), nil
	}
	store := fakeStore(fakeDatabase{begin: func(context.Context) (transaction, error) { began = true; return tx, nil }})
	store.limits = DefaultLimits()
	store.limits.MaxResultBytes = budget
	instance, history, err := store.CompareAndTransition(t.Context(), "i", 0, result, time.Unix(1, 0))
	if accepted {
		if err != nil || !committed || persisted != 1 || instance.State != result.Next || !reflect.DeepEqual(history.Result, result) {
			t.Fatalf("exact aggregate refused/changed: budget=%d err=%v", budget, err)
		}
	} else if !errors.Is(err, statemachine.ErrLimitExceeded) || began || committed || instance.ID != "" || history.InstanceID != "" {
		t.Fatalf("aggregate crossed preflight: budget=%d err=%v", budget, err)
	}
	if rolledBack != began {
		t.Fatal("aggregate transaction cleanup missing")
	}
}

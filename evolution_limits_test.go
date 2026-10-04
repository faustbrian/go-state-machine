package statemachine_test

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	statemachine "github.com/faustbrian/go-state-machine/v2"
)

func TestCompileEvolutionRejectsExcessMigrations(t *testing.T) {
	migrations := make([]statemachine.Migration[string, string], 257)
	for index := range migrations {
		migrations[index] = statemachine.Migration[string, string]{
			From: statemachine.Version("version-" + strconv.Itoa(index)),
			To:   statemachine.Version("version-" + strconv.Itoa(index+1)),
		}
	}

	evolution, err := statemachine.CompileEvolution(migrations)
	if !errors.Is(err, statemachine.ErrLimitExceeded) || evolution != nil {
		t.Fatalf("CompileEvolution(257 migrations) = (nonNil=%t, err=%v), want (nil, ErrLimitExceeded)", evolution != nil, err)
	}
}

func TestEvolutionRejectsExcessHistoryBeforeHooks(t *testing.T) {
	hookCalls := 0
	evolution, err := statemachine.CompileEvolution([]statemachine.Migration[string, string]{
		{
			From: "v1", To: "v2",
			State: func(state string) (string, error) {
				hookCalls++
				return state, nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	history := make([]statemachine.HistoryEntry[string, string], 10001)
	for index := range history {
		history[index].Result.DefinitionVersion = "v2"
	}

	snapshot, migrated, err := evolution.Migrate(context.Background(),
		statemachine.Snapshot[string]{DefinitionVersion: "v1", State: "ready"},
		history, "v2")
	if !errors.Is(err, statemachine.ErrLimitExceeded) || snapshot != (statemachine.Snapshot[string]{}) || migrated != nil || hookCalls != 0 {
		t.Fatalf("Migrate(10001 entries) = (snapshot=%#v, history=%d, err=%v, hookCalls=%d), want zero/nil ErrLimitExceeded before hooks", snapshot, len(migrated), err, hookCalls)
	}
}

func TestEvolutionRejectsLateInputExcessBeforeAnyHooks(t *testing.T) {
	sharedPayload := make([]byte, 1<<20)
	tests := []struct {
		name   string
		result statemachine.Result[string, string]
	}{
		{
			name:   "version bytes",
			result: statemachine.Result[string, string]{DefinitionVersion: statemachine.Version(strings.Repeat("x", 257))},
		},
		{
			name: "effect count",
			result: statemachine.Result[string, string]{
				DefinitionVersion: "v2", Effects: make([]statemachine.Effect, 10001),
			},
		},
		{
			name: "single payload bytes",
			result: statemachine.Result[string, string]{
				DefinitionVersion: "v2", Effects: []statemachine.Effect{{Payload: make([]byte, (1<<20)+1)}},
			},
		},
		{
			name: "aggregate payload occurrence bytes",
			result: statemachine.Result[string, string]{
				DefinitionVersion: "v2", Effects: append(sharedEffects(sharedPayload, 16), statemachine.Effect{Payload: []byte{1}}),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hookCalls := 0
			evolution, err := statemachine.CompileEvolution([]statemachine.Migration[string, string]{
				{From: "v1", To: "v2", State: func(state string) (string, error) {
					hookCalls++
					return state, nil
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			history := []statemachine.HistoryEntry[string, string]{
				{Result: statemachine.Result[string, string]{DefinitionVersion: "v1"}},
				{Result: test.result},
			}
			snapshot, migrated, err := evolution.Migrate(context.Background(),
				statemachine.Snapshot[string]{DefinitionVersion: "v1"}, history, "v2")
			if !errors.Is(err, statemachine.ErrLimitExceeded) || snapshot != (statemachine.Snapshot[string]{}) || migrated != nil || hookCalls != 0 {
				t.Fatalf("Migrate(late %s) = (snapshotNonzero=%t, history=%d, err=%v, hookCalls=%d), want zero/nil ErrLimitExceeded before hooks", test.name, snapshot != (statemachine.Snapshot[string]{}), len(migrated), err, hookCalls)
			}
		})
	}
}

func sharedEffects(payload []byte, count int) []statemachine.Effect {
	effects := make([]statemachine.Effect, count)
	for index := range effects {
		effects[index].Payload = payload
	}
	return effects
}

func TestEvolutionMigrationStepBudgetCountsSnapshotAndEveryHistoryPath(t *testing.T) {
	migrations := make([]statemachine.Migration[string, string], 10)
	hookCalls := 0
	for index := range migrations {
		migrations[index] = statemachine.Migration[string, string]{
			From: statemachine.Version("v" + strconv.Itoa(index)),
			To:   statemachine.Version("v" + strconv.Itoa(index+1)),
		}
	}
	migrations[0].State = func(value string) (string, error) {
		hookCalls++
		return value, nil
	}
	evolution, err := statemachine.CompileEvolution(migrations)
	if err != nil {
		t.Fatal(err)
	}
	history := make([]statemachine.HistoryEntry[string, string], 10000)
	for index := range history {
		history[index].Result.DefinitionVersion = "v0"
	}

	_, migrated, err := evolution.Migrate(context.Background(),
		statemachine.Snapshot[string]{DefinitionVersion: "v10"}, history, "v10")
	if err != nil || len(migrated) != 10000 || migrated[9999].Result.DefinitionVersion != "v10" {
		t.Fatalf("exactly 100000 edge applications = (history=%d, err=%v), want accepted", len(migrated), err)
	}
	if hookCalls != 20000 {
		t.Fatalf("exact-budget hook calls = %d, want 20000", hookCalls)
	}
	hookCalls = 0
	snapshot, migrated, err := evolution.Migrate(context.Background(),
		statemachine.Snapshot[string]{DefinitionVersion: "v9"}, history, "v10")
	if !errors.Is(err, statemachine.ErrLimitExceeded) || snapshot != (statemachine.Snapshot[string]{}) || migrated != nil || hookCalls != 0 {
		t.Fatalf("100001 edge applications = (nonzero=%t, history=%d, err=%v, hooks=%d), want zero/nil ErrLimitExceeded before hooks", snapshot != (statemachine.Snapshot[string]{}), len(migrated), err, hookCalls)
	}
}

func TestEvolutionLimitsAreCompleteAndCopied(t *testing.T) {
	if evolution, err := statemachine.CompileEvolutionWithLimits([]statemachine.Migration[string, string]{{From: "v1", To: "v2"}},
		statemachine.EvolutionLimits{MaxMigrations: 1}); !errors.Is(err, statemachine.ErrInvalidEvolution) || evolution != nil {
		t.Fatalf("partial limits = (nonNil=%t, err=%v), want invalid", evolution != nil, err)
	}
	negative := statemachine.DefaultEvolutionLimits()
	negative.MaxHistoryEntries = -1
	if evolution, err := statemachine.CompileEvolutionWithLimits([]statemachine.Migration[string, string]{{From: "v1", To: "v2"}}, negative); !errors.Is(err, statemachine.ErrInvalidEvolution) || evolution != nil {
		t.Fatalf("negative limits = (nonNil=%t, err=%v), want invalid", evolution != nil, err)
	}

	limits := statemachine.DefaultEvolutionLimits()
	limits.MaxHistoryEntries = 1
	evolution, err := statemachine.CompileEvolutionWithLimits([]statemachine.Migration[string, string]{{From: "v1", To: "v2"}}, limits)
	if err != nil {
		t.Fatal(err)
	}
	limits.MaxHistoryEntries = 2
	history := []statemachine.HistoryEntry[string, string]{
		{Result: statemachine.Result[string, string]{DefinitionVersion: "v2"}},
		{Result: statemachine.Result[string, string]{DefinitionVersion: "v2"}},
	}
	if _, _, err := evolution.Migrate(context.Background(), statemachine.Snapshot[string]{DefinitionVersion: "v2"}, history, "v2"); !errors.Is(err, statemachine.ErrLimitExceeded) {
		t.Fatalf("mutated caller limits error = %v, want original one-entry bound", err)
	}
	if _, migrated, err := evolution.Migrate(context.Background(), statemachine.Snapshot[string]{DefinitionVersion: "v2"}, history[:1], "v2"); err != nil || len(migrated) != 1 {
		t.Fatalf("custom exact bound = (history=%d, err=%v), want one entry", len(migrated), err)
	}
	large := statemachine.DefaultEvolutionLimits()
	large.MaxMigrations = 257
	migrations := make([]statemachine.Migration[string, string], 257)
	for index := range migrations {
		migrations[index] = statemachine.Migration[string, string]{
			From: statemachine.Version("version-" + strconv.Itoa(index)),
			To:   statemachine.Version("version-" + strconv.Itoa(index+1)),
		}
	}
	if compiled, err := statemachine.CompileEvolutionWithLimits(migrations, large); err != nil || compiled == nil {
		t.Fatalf("caller-selected 257-record bound = (nonNil=%t, err=%v), want accepted", compiled != nil, err)
	}
}

func TestEvolutionVersionByteBoundsAndErrorPrecedence(t *testing.T) {
	exact := statemachine.Version(strings.Repeat("é", 128))
	tooLong := exact + "x"
	for _, migration := range []statemachine.Migration[string, string]{
		{From: tooLong, To: "v2"},
		{From: "v1", To: tooLong},
	} {
		if evolution, err := statemachine.CompileEvolution([]statemachine.Migration[string, string]{migration}); !errors.Is(err, statemachine.ErrLimitExceeded) || evolution != nil {
			t.Fatalf("257-byte migration version = (nonNil=%t, err=%v), want limit", evolution != nil, err)
		}
	}
	evolution, err := statemachine.CompileEvolution([]statemachine.Migration[string, string]{{From: exact, To: "v2"}})
	if err != nil {
		t.Fatalf("256-byte migration version: %v", err)
	}
	snapshot, _, err := evolution.Migrate(context.Background(), statemachine.Snapshot[string]{DefinitionVersion: exact}, nil, "v2")
	if err != nil || snapshot.DefinitionVersion != "v2" {
		t.Fatalf("256-byte snapshot version = (%q, %v), want v2", snapshot.DefinitionVersion, err)
	}
	for _, input := range []struct {
		name     string
		snapshot statemachine.Snapshot[string]
		target   statemachine.Version
	}{
		{name: "target", snapshot: statemachine.Snapshot[string]{DefinitionVersion: "v2"}, target: tooLong},
		{name: "snapshot", snapshot: statemachine.Snapshot[string]{DefinitionVersion: tooLong}, target: "v2"},
	} {
		t.Run(input.name, func(t *testing.T) {
			got, history, err := evolution.Migrate(context.Background(), input.snapshot, nil, input.target)
			if !errors.Is(err, statemachine.ErrLimitExceeded) || got != (statemachine.Snapshot[string]{}) || history != nil {
				t.Fatalf("257-byte %s version = (nonzero=%t, history=%d, err=%v), want limit", input.name, got != (statemachine.Snapshot[string]{}), len(history), err)
			}
		})
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	oversizedHistory := make([]statemachine.HistoryEntry[string, string], 10001)
	if got, history, err := evolution.Migrate(canceled, statemachine.Snapshot[string]{DefinitionVersion: tooLong}, oversizedHistory, ""); !errors.Is(err, statemachine.ErrInvalidEvolution) || got != (statemachine.Snapshot[string]{}) || history != nil {
		t.Fatalf("empty target precedence = (nonzero=%t, history=%d, err=%v), want invalid", got != (statemachine.Snapshot[string]{}), len(history), err)
	}
	if got, history, err := evolution.Migrate(canceled, statemachine.Snapshot[string]{DefinitionVersion: tooLong}, oversizedHistory, "v2"); !errors.Is(err, context.Canceled) || got != (statemachine.Snapshot[string]{}) || history != nil {
		t.Fatalf("pre-canceled precedence = (nonzero=%t, history=%d, err=%v), want canceled", got != (statemachine.Snapshot[string]{}), len(history), err)
	}
}

func TestEvolutionCountsSharedPayloadOccurrencesAndCopiesAcceptedEffects(t *testing.T) {
	evolution, err := statemachine.CompileEvolution[string, string](nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 1<<20)
	payload[0] = 7
	history := []statemachine.HistoryEntry[string, string]{{Result: statemachine.Result[string, string]{
		DefinitionVersion: "v1", Effects: sharedEffects(payload, 16),
	}}}
	_, migrated, err := evolution.Migrate(context.Background(), statemachine.Snapshot[string]{DefinitionVersion: "v1"}, history, "v1")
	if err != nil || len(migrated) != 1 || len(migrated[0].Result.Effects) != 16 {
		t.Fatalf("exact aggregate payload = (history=%d, err=%v), want accepted", len(migrated), err)
	}
	migrated[0].Result.Effects[0].Payload[0] = 9
	if payload[0] != 7 || migrated[0].Result.Effects[1].Payload[0] != 7 {
		t.Fatal("accepted effect payloads alias input or each other")
	}
}

func TestEvolutionInclusiveDefaultMigrationAndEffectCounts(t *testing.T) {
	migrations := make([]statemachine.Migration[string, string], 256)
	for index := range migrations {
		migrations[index] = statemachine.Migration[string, string]{
			From: statemachine.Version("v" + strconv.Itoa(index)),
			To:   statemachine.Version("v" + strconv.Itoa(index+1)),
		}
	}
	evolution, err := statemachine.CompileEvolution(migrations)
	if err != nil {
		t.Fatalf("exact 256 migrations: %v", err)
	}
	history := []statemachine.HistoryEntry[string, string]{{Result: statemachine.Result[string, string]{
		DefinitionVersion: "v256", Effects: make([]statemachine.Effect, 10000),
	}}}
	_, migrated, err := evolution.Migrate(context.Background(),
		statemachine.Snapshot[string]{DefinitionVersion: "v256"}, history, "v256")
	if err != nil || len(migrated) != 1 || len(migrated[0].Result.Effects) != 10000 {
		t.Fatalf("exact 10000 effects = (history=%d, err=%v), want accepted", len(migrated), err)
	}
}

func TestEvolutionLimitErrorsDoNotRenderInputValues(t *testing.T) {
	const secret = "attacker-secret-marker"
	evolution, err := statemachine.CompileEvolution[string, string](nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := statemachine.Snapshot[string]{State: secret, DefinitionVersion: "v1"}
	history := []statemachine.HistoryEntry[string, string]{{Result: statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: secret, Event: secret,
		Effects: []statemachine.Effect{{Kind: secret, Payload: []byte(strings.Repeat(secret, 70000))}},
	}}}
	_, _, err = evolution.Migrate(context.Background(), snapshot, history, "v1")
	if !errors.Is(err, statemachine.ErrLimitExceeded) || strings.Contains(err.Error(), secret) {
		t.Fatalf("limit error = %v, want redacted ErrLimitExceeded", err)
	}
}

func TestEvolutionStopsBetweenHistoryHooksAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := []string{}
	evolution, err := statemachine.CompileEvolution([]statemachine.Migration[string, string]{
		{From: "v1", To: "v2", State: func(value string) (string, error) {
			calls = append(calls, value)
			if value == "previous" {
				cancel()
			}
			return value, nil
		}, Event: func(value string) (string, error) {
			calls = append(calls, value)
			return value, nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	history := []statemachine.HistoryEntry[string, string]{{Result: statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: "previous", Next: "next", Event: "event",
	}}}
	snapshot, migrated, err := evolution.Migrate(ctx, statemachine.Snapshot[string]{DefinitionVersion: "v2"}, history, "v2")
	if !errors.Is(err, context.Canceled) || snapshot != (statemachine.Snapshot[string]{}) || migrated != nil || len(calls) != 1 || calls[0] != "previous" {
		t.Fatalf("cancel after previous hook = (nonzero=%t, history=%d, err=%v, calls=%v), want only previous hook and no output", snapshot != (statemachine.Snapshot[string]{}), len(migrated), err, calls)
	}
}

func TestEvolutionAcceptedOutputRemainsValidForExplicitConsumer(t *testing.T) {
	evolution, err := statemachine.CompileEvolution([]statemachine.Migration[string, string]{
		{From: "v1", To: "v2", State: func(value string) (string, error) {
			if value == "pending" {
				return "awaiting", nil
			}
			return value, nil
		}, Event: func(value string) (string, error) {
			if value == "pay" {
				return "capture", nil
			}
			return value, nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, time.September, 22, 9, 0, 0, 0, time.UTC)
	snapshot := statemachine.Snapshot[string]{
		InstanceID: "order-1", State: "pending", DefinitionVersion: "v1", CreatedAt: when,
	}
	history := []statemachine.HistoryEntry[string, string]{{
		InstanceID: "order-1", Sequence: 1, OccurredAt: when.Add(time.Minute),
		Result: statemachine.Result[string, string]{
			DefinitionVersion: "v1", Previous: "pending", Next: "paid",
			Event: "pay", TransitionID: "capture-order",
			Metadata: statemachine.Metadata{CorrelationID: "flow-1", CausationID: "cause-1"},
			Effects:  []statemachine.Effect{{Kind: "receipt", Payload: []byte("ok")}},
		},
	}}
	migratedSnapshot, migratedHistory, err := evolution.Migrate(context.Background(), snapshot, history, "v2")
	if err != nil {
		t.Fatal(err)
	}
	wantSnapshot := snapshot
	wantSnapshot.State = "awaiting"
	wantSnapshot.DefinitionVersion = "v2"
	wantHistory := history[0]
	wantHistory.Result.DefinitionVersion = "v2"
	wantHistory.Result.Previous = "awaiting"
	wantHistory.Result.Event = "capture"
	if migratedSnapshot != wantSnapshot || len(migratedHistory) != 1 || !reflect.DeepEqual(migratedHistory[0], wantHistory) {
		t.Fatalf("migrated values differ from independently specified snapshot/history")
	}
	if snapshot.State != "pending" || history[0].Result.Event != "pay" {
		t.Fatal("migration changed caller-owned input")
	}
	final, err := statemachine.ValidateHistory(migratedSnapshot, migratedHistory)
	if err != nil || final.State != "paid" || final.LockVersion != 1 {
		t.Fatalf("structural consumer = (state=%q, version=%d, err=%v), want paid at 1", final.State, final.LockVersion, err)
	}
	machine, err := statemachine.Compile(statemachine.Definition[string, string, struct{}]{
		Version: "v2", Initial: "awaiting",
		States: []statemachine.StateDefinition[string]{
			{State: "awaiting"}, {State: "paid", Terminal: true},
		},
		Transitions: []statemachine.TransitionDefinition[string, string, struct{}]{
			{ID: "capture-order", Sources: []string{"awaiting"}, Event: "capture", To: "paid"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	final, err = machine.ValidateHistory(migratedSnapshot, migratedHistory)
	if err != nil || final.State != "paid" || final.DefinitionVersion != "v2" {
		t.Fatalf("definition consumer = (state=%q, version=%q, err=%v), want paid/v2", final.State, final.DefinitionVersion, err)
	}
	oldMachine, err := statemachine.Compile(statemachine.Definition[string, string, struct{}]{
		Version: "v1", Initial: "pending",
		States: []statemachine.StateDefinition[string]{
			{State: "pending"}, {State: "paid", Terminal: true},
		},
		Transitions: []statemachine.TransitionDefinition[string, string, struct{}]{
			{ID: "capture-order", Sources: []string{"pending"}, Event: "pay", To: "paid"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oldMachine.ValidateHistory(migratedSnapshot, migratedHistory); err == nil {
		t.Fatal("older definition accepted migrated v2 history")
	}
}

type evolutionState int
type evolutionEvent int

func TestEvolutionSupportsComparableNonStringValues(t *testing.T) {
	evolution, err := statemachine.CompileEvolution([]statemachine.Migration[evolutionState, evolutionEvent]{
		{From: "v1", To: "v2", State: func(value evolutionState) (evolutionState, error) {
			return value + 1, nil
		}, Event: func(value evolutionEvent) (evolutionEvent, error) {
			return value + 1, nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	history := []statemachine.HistoryEntry[evolutionState, evolutionEvent]{{
		Result: statemachine.Result[evolutionState, evolutionEvent]{
			DefinitionVersion: "v1", Previous: 1, Next: 2, Event: 3,
		},
	}}
	snapshot, migrated, err := evolution.Migrate(context.Background(),
		statemachine.Snapshot[evolutionState]{DefinitionVersion: "v1", State: 1}, history, "v2")
	if err != nil || snapshot.State != 2 || len(migrated) != 1 ||
		migrated[0].Result.Previous != 2 || migrated[0].Result.Next != 3 || migrated[0].Result.Event != 4 {
		t.Fatalf("non-string migration = (snapshot=%d, history=%d, err=%v), want transformed values", snapshot.State, len(migrated), err)
	}
}

func TestEvolutionRetryReinvokesPureHooksWithoutRetainingOutput(t *testing.T) {
	calls := 0
	evolution, err := statemachine.CompileEvolution([]statemachine.Migration[string, string]{
		{From: "v1", To: "v2", State: func(value string) (string, error) {
			calls++
			return value + "-v2", nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	input := statemachine.Snapshot[string]{DefinitionVersion: "v1", State: "ready"}
	first, firstHistory, err := evolution.Migrate(context.Background(), input, nil, "v2")
	if err != nil {
		t.Fatal(err)
	}
	second, secondHistory, err := evolution.Migrate(context.Background(), input, nil, "v2")
	if err != nil || first != second || len(firstHistory) != 0 || len(secondHistory) != 0 || calls != 2 || input.State != "ready" {
		t.Fatalf("pure-hook retry = (same=%t, calls=%d, err=%v), want same output and two invocations", first == second, calls, err)
	}
}

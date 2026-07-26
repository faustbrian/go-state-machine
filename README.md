# state-machine

`state-machine` is a typed, deterministic state-machine library for Go.
Compiled definitions select transitions and plan effects without performing
I/O. Optional packages execute effects, persist state and history, publish a
durable outbox, and export diagrams.

It is intentionally not a workflow engine, scheduler, rule engine, queue, saga
framework, or dependency-injection container.

## Quick start

```go
package main

import (
	"context"
	"fmt"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

type State string
type Event string

const (
	Pending State = "pending"
	Paid    State = "paid"
	Pay     Event = "pay"
)

func main() {
	machine, err := statemachine.Compile(
		statemachine.Definition[State, Event, int]{
			Version: "v1",
			Initial: Pending,
			States: []statemachine.StateDefinition[State]{
				{State: Pending},
				{State: Paid, Terminal: true},
			},
			Transitions: []statemachine.TransitionDefinition[State, Event, int]{
				{
					ID: "pay-order", Sources: []State{Pending},
					Event: Pay, To: Paid,
					Effects: []statemachine.Effect{{Kind: "send-receipt"}},
				},
			},
		},
	)
	if err != nil {
		panic(err)
	}

	result, err := machine.Transition(
		context.Background(), Pending, Pay, 42,
		statemachine.Metadata{CorrelationID: "order-42"},
	)
	if err != nil {
		panic(err)
	}

	fmt.Println(result.Next, result.Effects[0].Kind)
	// Output: paid send-receipt
}
```

`Transition` performs no I/O. The returned effects are inert data. Execute them
explicitly with `runner`, or persist them atomically with a transition through
the PostgreSQL store and publish them with `outbox`.

## Packages

- Root package `statemachine`: definitions, compilation, pure transitions,
  diagnostics, replay, history validation, resource limits, and evolution.
- `runner`: explicit ordered effect execution and outcome recording.
- `memory`: concurrency-safe optimistic store for ephemeral use and tests.
- `postgres`: durable state, history, snapshots, leases, and atomic outbox.
- `outbox`: observable at-least-once publication relay.
- `diagram`: deterministic Mermaid and Graphviz export with checked imports.
- `statemachinetest`: reusable store and runner conformance tests.

## Guarantees

- A compiled machine is immutable and safe for concurrent use.
- Exact source/event transitions take precedence over wildcard transitions.
- Ambiguous definitions fail during compilation.
- Terminal states reject all transitions, including wildcards.
- Guards are evaluated in definition order and must be side-effect free.
- Exit, transition, and entry effects are returned in that order.
- Self-transitions use the same external-transition ordering.
- Guard and effect-handler panics are contained without exposing panic values.
- PostgreSQL commits current state, history, and effect outbox rows in one
  transaction.
- Outbox publication is at least once. Exactly once is not claimed.

## Documentation

Start with the [documentation index](docs/README.md). It links the API
reference, architecture, persistence, migration, replay, concurrency, adoption,
FAQ, and integration guides.

## Local verification

All CI gates are exposed through the `Makefile`:

```sh
make check
```

The PostgreSQL integration and race gates require a working Docker daemon.

## License

MIT. See [LICENSE](LICENSE).

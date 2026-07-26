# Quick start

Define distinct state and event types, compile once, and reuse the resulting
machine concurrently.

```go
type OrderState string
type OrderEvent string

const (
    Pending OrderState = "pending"
    Paid    OrderState = "paid"
    Pay     OrderEvent = "pay"
)

machine, err := statemachine.Compile(
    statemachine.Definition[OrderState, OrderEvent, Account]{
        Version: "v1",
        Initial: Pending,
        States: []statemachine.StateDefinition[OrderState]{
            {State: Pending},
            {State: Paid, Terminal: true},
        },
        Transitions: []statemachine.TransitionDefinition[OrderState, OrderEvent, Account]{
            {
                ID: "pay-order", Sources: []OrderState{Pending},
                Event: Pay, To: Paid,
                Guards: []statemachine.Guard[Account]{hasFunds},
                Effects: []statemachine.Effect{{Kind: "capture-payment"}},
            },
        },
    },
)
```

Compilation validates state identity, reachability, terminal-state rules,
transition identifiers, exact/wildcard ambiguity, effects, and resource limits.
A `DiagnosticsError` contains every independently detectable problem.

Calculate a transition:

```go
result, err := machine.Transition(
    ctx, Pending, Pay, account,
    statemachine.Metadata{
        CorrelationID: "order-42",
        CausationID: "request-9",
    },
)
```

No guard or transition lookup executes effects. `result.Effects` is an ordered
plan. Persist it, hand it to `runner`, or translate it into an application
command explicitly.

Use `CompileWithLimits` when an application needs tighter resource bounds than
`DefaultLimits`. See runnable examples in `examples_test.go`.

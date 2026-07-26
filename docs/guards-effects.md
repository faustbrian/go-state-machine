# Guards and effects

## Guards

A guard receives `context.Context` and the definition's typed context value. It
returns `nil` to accept or `*Rejection` to decline.

```go
func hasFunds(ctx context.Context, account Account) *statemachine.Rejection {
    if account.Available < account.Required {
        return &statemachine.Rejection{
            Code: "insufficient_funds",
            Message: "available balance is below the required amount",
        }
    }
    return nil
}
```

Guards must be deterministic and side-effect free. Do not mutate context data,
write logs as business behavior, call a database, reserve funds, publish, or
read an ambient clock. Calculate such work after selection as explicit effects.

An exact transition whose guard rejects does not fall through to a wildcard.
`GuardRejectedError` preserves the transition ID and structured rejection.
Guard panics become `GuardPanicError`; panic values are omitted. Cancellation
is checked before selection and before each guard.

Use `CheckedGuard` when evaluation can fail operationally. A checked guard may
return either a domain `Rejection` or an error; error text is not rendered by
the state machine, while `errors.Is` and `errors.As` still reach the cause.

Reference-bearing contexts MUST configure `Definition.CloneContext` with a
deep clone. The machine invokes it separately for every guard so mutation
cannot leak to the caller or another guard. Cloner and guard panics are
contained without exposing panic values.

## Effects

`Effect` contains a stable kind and opaque byte payload. Compilation deep-copies
payloads, and transition results receive fresh copies. Effect lookup never
executes a handler.

Effect order is always:

1. current-state exit effects;
2. chosen-transition effects;
3. destination-state entry effects.

The same order applies to self-transitions.

`runner.Runner` invokes a plan serially and stops after the first unsuccessful
outcome. It checks cancellation, contains handler panics, prevents nested calls
on the same runner/context, classifies retryable versus permanent failures, and
optionally records every attempt. A runner call invokes each reached effect at
most once, but process crashes and durable delivery require outbox semantics.

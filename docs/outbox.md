# Outbox integration

PostgreSQL inserts planned effects in `state_machine_outbox` in the same
transaction as current state and history. `postgres.Store` also implements the
`outbox.Store` lease contract.

```go
relay, err := outbox.NewRelay(outbox.RelayOptions{
    Store: store,
    Publisher: publisher,
    Clock: time.Now,
    Classify: classifyPublishError,
    RetryDelay: retryBackoff,
})

result, err := relay.RunOnce(ctx, outbox.ClaimRequest{
    Owner: workerID,
    Limit: 100,
    LeaseDuration: 30 * time.Second,
})
```

Claims use ordered `FOR UPDATE SKIP LOCKED` selection and unique lease tokens.
Publishing succeeds before acknowledgement. If the process crashes between
those steps, the lease expires and the message can be delivered again.
Therefore delivery is observably at least once, never exactly once.

Use stable message IDs with `idempotency` or equivalent consumer-side
deduplication. Applications may adapt `outbox.Message` to `outbox`, publish
through `queue`, classify failures with `retry`, and propagate IDs using
`correlation`. Those integrations remain explicit; the root package does not
import them or locate them through a container.

Retryable failures are rescheduled once per relay pass. Permanent failures are
dead-lettered. Publisher panics are contained as `ErrPublisherPanic`. A stale
or completed lease returns `ErrLeaseLost`.

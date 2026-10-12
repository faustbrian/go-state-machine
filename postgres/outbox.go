package postgres

import (
	"context"
	"fmt"
	"time"

	statemachine "github.com/faustbrian/go-state-machine/v2"
	"github.com/faustbrian/go-state-machine/v2/outbox"
)

const (
	maxClaimBatch = 1_000
)

// Claim accepts a nonempty owner, positive lease duration and 1..1000 records.
// It uses caller Clock and FOR UPDATE SKIP LOCKED, sorted by availability,
// occurrence time and ID, not effect index or global delivery order. Candidates
// are budgeted before lease updates; NewID must provide fresh unique tokens.
// Expiry makes a record reclaimable; completion is token-fenced, not time-checked.
func (store *Store[S, E]) Claim(ctx context.Context, request outbox.ClaimRequest) ([]outbox.Claim, error) {
	if request.Owner == "" || request.Limit <= 0 || request.Limit > maxClaimBatch || request.LeaseDuration <= 0 {
		return nil, outbox.ErrInvalidClaim
	}
	limits := store.persistenceLimits()
	if len(request.Owner) > limits.MaxIdentifierBytes {
		return nil, fmt.Errorf("%w: outbox owner", statemachine.ErrLimitExceeded)
	}
	now := store.clock()
	leasedUntil := now.Add(request.LeaseDuration)
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return nil, wrapError("begin outbox claim", err)
	}
	defer rollback(tx)
	rows, err := tx.Query(ctx, fmt.Sprintf(`
SELECT id, instance_id, sequence, effect_index, kind, payload, occurred_at,
       attempts
FROM %s.state_machine_outbox
WHERE published_at IS NULL
  AND dead_lettered_at IS NULL
  AND available_at <= $1
  AND (leased_until IS NULL OR leased_until <= $1)
ORDER BY available_at, occurred_at, id
FOR UPDATE SKIP LOCKED
LIMIT $2`, store.schema), now, request.Limit)
	if err != nil {
		return nil, wrapError("select outbox claims", err)
	}
	type candidate struct {
		id         string
		instanceID string
		sequence   uint64
		index      int
		kind       string
		payload    []byte
		occurredAt time.Time
		attempts   int
	}
	candidates := make([]candidate, 0, request.Limit)
	remainingBytes := limits.MaxClaimBytes
	for rows.Next() {
		var item candidate
		var sequence int64
		if err := rows.Scan(&item.id, &item.instanceID, &sequence, &item.index, &item.kind, &item.payload, &item.occurredAt, &item.attempts); err != nil {
			rows.Close()
			return nil, wrapError("scan outbox claim", err)
		}
		if sequence <= 0 {
			rows.Close()
			return nil, statemachine.ErrInvalidStoreInput
		}
		item.sequence = uint64(sequence)
		if item.id == "" || item.instanceID == "" || item.index < 0 || item.kind == "" || item.attempts < 0 {
			rows.Close()
			return nil, statemachine.ErrInvalidStoreInput
		}
		if len(item.id) > limits.MaxIdentifierBytes || len(item.instanceID) > limits.MaxInstanceIDBytes ||
			len(item.kind) > limits.MaxIdentifierBytes || len(item.payload) > limits.Machine.MaxEffectPayloadBytes {
			rows.Close()
			return nil, fmt.Errorf("%w: outbox message", statemachine.ErrLimitExceeded)
		}
		if !consumeSize(&remainingBytes, len(item.id), 1) ||
			!consumeSize(&remainingBytes, len(item.instanceID), 1) ||
			!consumeSize(&remainingBytes, len(item.kind), 1) ||
			!consumeSize(&remainingBytes, len(item.payload), 1) {
			rows.Close()
			return nil, fmt.Errorf("%w: outbox claim bytes", statemachine.ErrLimitExceeded)
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, wrapError("iterate outbox claims", err)
	}
	rows.Close()

	claims := make([]outbox.Claim, 0, len(candidates))
	for _, item := range candidates {
		token := store.newID()
		if token == "" {
			return nil, fmt.Errorf("%w: empty lease token", outbox.ErrInvalidClaim)
		}
		if len(token) > limits.MaxIdentifierBytes {
			return nil, fmt.Errorf("%w: outbox lease token", statemachine.ErrLimitExceeded)
		}
		_, err := tx.Exec(ctx, fmt.Sprintf(`
UPDATE %s.state_machine_outbox
SET lease_owner = $1, lease_token = $2, leased_until = $3,
    attempts = attempts + 1
WHERE id = $4`, store.schema), request.Owner, token, leasedUntil, item.id)
		if err != nil {
			return nil, wrapError("lease outbox message", err)
		}
		claims = append(claims, outbox.Claim{
			Message: outbox.Message{
				ID: item.id, InstanceID: statemachineInstanceID(item.instanceID),
				Sequence: item.sequence, Index: item.index,
				Effect:     statemachineEffect(item.kind, item.payload),
				OccurredAt: item.occurredAt, Attempts: item.attempts + 1,
			},
			Token: token, LeasedUntil: leasedUntil,
		})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, wrapError("commit outbox claim", err)
	}
	return claims, nil
}

// MarkPublished completes a still-current unfinished token after publication.
// LeasedUntil is not checked: an expired token can complete before reclamation.
// Replaced/completed tokens return ErrLeaseLost.
func (store *Store[S, E]) MarkPublished(ctx context.Context, ref outbox.LeaseRef, at time.Time) error {
	return store.finishLease(ctx, ref, `published_at = $3`, at, nil)
}

// Retry releases a still-current unfinished token and sets availability time.
// It does not sleep or check lease expiry; non-nil cause persists only redacted.
func (store *Store[S, E]) Retry(ctx context.Context, ref outbox.LeaseRef, availableAt time.Time, cause error) error {
	return store.finishLease(ctx, ref, `available_at = $3`, availableAt, cause)
}

// DeadLetter removes a still-current unfinished token from normal claiming.
// It does not check lease expiry; non-nil cause persists only redacted.
func (store *Store[S, E]) DeadLetter(ctx context.Context, ref outbox.LeaseRef, at time.Time, cause error) error {
	return store.finishLease(ctx, ref, `dead_lettered_at = $3`, at, cause)
}

func (store *Store[S, E]) finishLease(ctx context.Context, ref outbox.LeaseRef, assignment string, value time.Time, cause error) error {
	if ref.ID == "" || ref.Token == "" {
		return outbox.ErrInvalidClaim
	}
	if len(ref.ID) > store.persistenceLimits().MaxIdentifierBytes || len(ref.Token) > store.persistenceLimits().MaxIdentifierBytes {
		return fmt.Errorf("%w: outbox lease reference", statemachine.ErrLimitExceeded)
	}
	// Time controls reclaim eligibility in Claim. Completion fences the current
	// token and unfinished status, but has no expiry predicate here.
	// Reclaiming with a fresh token prevents old-generation completion.
	errorText := boundedErrorText(cause)
	tag, err := store.pool.Exec(ctx, fmt.Sprintf(`
UPDATE %s.state_machine_outbox
SET %s, last_error = NULLIF($4, ''), lease_owner = NULL,
    lease_token = NULL, leased_until = NULL
WHERE id = $1 AND lease_token = $2 AND published_at IS NULL
  AND dead_lettered_at IS NULL`, store.schema, assignment), ref.ID, ref.Token, value, errorText)
	if err != nil {
		return wrapError("finish outbox lease", err)
	}
	if tag.RowsAffected() == 0 {
		return outbox.ErrLeaseLost
	}
	return nil
}

func boundedErrorText(cause error) string {
	if cause == nil {
		return ""
	}
	return "redacted"
}

// These small adapters keep the public outbox package independent of the
// persistence implementation while avoiding payload aliasing.
func statemachineInstanceID(value string) statemachine.InstanceID {
	return statemachine.InstanceID(value)
}

func statemachineEffect(kind string, payload []byte) statemachine.Effect {
	return statemachine.Effect{Kind: kind, Payload: append([]byte(nil), payload...)}
}

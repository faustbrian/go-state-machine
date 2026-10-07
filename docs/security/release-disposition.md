# State Machine published-source security disposition

Reviewed: 2026-10-07. Owner: go-state-machine maintainers.

## Observed released sources

The public original module has releases v1.0.0, v1.0.1 and v1.0.2. The
reviewed tag sources are respectively
`f091a2f3ece60a2df7eb08fd082207a0d50c1dc4`,
`864a90281a035f6cbe057af882190e0345656775` and
`5f303107f147d529c264d4894428f2401586ef33`.
These source identities are not deployment or exploitability evidence.

Across those releases, direct runner execution has no v2 effect
count or aggregate-payload admission before record allocation. Compile and
evolution also lack v2's additional pre-copy and aggregate
admission controls. The existing v1 state/transition limits are not universal
byte or callback-work bounds. Applications accepting external definitions,
history or effect plans must validate their own input before using v1.

The released v1 guard-rejection error string includes the supplied rejection
code and message; other public diagnostics can include transition identifiers.
Those are application-supplied values and may be sensitive. Applications must
not place secrets in them or expose them to unauthorized logs or readers.
The v2 default-safe error strings do not rewrite any v1 artifact.

## Published corrective controls and adoption

The current `/v2` source adds the bounded compilation, evolution, direct runner
and PostgreSQL controls and default-safe error strings described in the
2.0.0 changelog and [threat model](threat-model-v2.md).
[Release v2.0.0](https://github.com/faustbrian/go-state-machine/releases/tag/v2.0.0)
of `github.com/faustbrian/go-state-machine/v2` was published on 2026-10-07
from source `65b942580100bc6a47daeb8cd158645cf62f694f`. Its signed tag and
official public module source were verified, and a clean public consumer
passed finite runner, compilation, diagnostic and dependency-admission checks.
Maintained ecosystem adoption remains separate from publication.
No corrected original-module patch or backport is asserted here.

Migrate imports to `/v2`, select complete finite limits for
custom workloads, review typed-error handling and persisted-error changes,
and verify the application's own persistence and replay contracts. Until an
application migrates, retain its v1 admission and diagnostic precautions;
do not consume the v2 source under the v1 module with a local replacement.

## Impact and disclosure boundary

The source behavior above is confirmed. Whether an adopting deployment gives
an untrusted actor control over those values, or exposes sensitive diagnostics
to unauthorized readers, is not established by this review. No remote attack,
customer incident, CVE, GitHub security advisory, universal severity or
deployment-wide affected range is asserted. Maintainers must triage a concrete
report using the private
policy in [SECURITY](../../SECURITY.md), record reachable affected modules and
versions, and coordinate an advisory when a vulnerability is confirmed.
Reopen this disposition on such a report, new published source evidence, a v1
backport or a changed v2 corrective contract.

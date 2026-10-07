# Security policy

Security fixes are provided for the latest released major version and the Go
versions supported by that release.

The default branch currently prepares an unpublished v2 release candidate.
Released v1 remains the supported production line until v2 is published.
The candidate's security controls do not change already published v1 artifacts.

Do not open a public issue for a suspected vulnerability. Use the repository's
[private security advisory](https://github.com/faustbrian/go-state-machine/security/advisories/new)
form. Include an impact summary, affected versions, reproduction, and suggested
mitigation. You should receive an acknowledgement within seven days.

Maintainers follow the immutable ecosystem
[vulnerability management policy](https://github.com/faustbrian/go-library-tools/blob/5ad0adb193a46306b4500b8b59cee9ec110fdee3/docs/ecosystem/security/vulnerability-management.md)
for severity, remediation targets, private evidence, embargo, advisories and
coordinated affected-module releases. The existing seven-day acknowledgement
target remains applicable to every report; Critical and High reports also
have the policy's stricter one- and two-business-day targets. Targets begin
when sufficient private evidence exists to reproduce or confidently bound the
report, and the maintainer communicates evidence gaps or target changes.

The [published-source disposition](docs/security/release-disposition.md)
separates observed v1 behavior, prepared v2 controls and unverified deployment
impact. A dated changelog or passing qualification does not itself establish
publication or a fixed version available to consumers.

Never include production state, event payloads, effect payloads, database
credentials, or correlation identifiers in a report unless they have been
irreversibly sanitized.

The repository threat model and accepted-risk register are maintained in
[`docs/security/threat-model-v2.md`](docs/security/threat-model-v2.md).

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

Never include production state, event payloads, effect payloads, database
credentials, or correlation identifiers in a report unless they have been
irreversibly sanitized.

The repository threat model and accepted-risk register are maintained in
[`docs/security/threat-model-v2.md`](docs/security/threat-model-v2.md).

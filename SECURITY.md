# Security policy

Security fixes are provided for the latest released major version and the Go
versions supported by that release.

Do not open a public issue for a suspected vulnerability. Use the repository's
[private security advisory](https://github.com/faustbrian/go-state-machine/security/advisories/new)
form. Include an impact summary, affected versions, reproduction, and suggested
mitigation. You should receive an acknowledgement within seven days.

Never include production state, event payloads, effect payloads, database
credentials, or correlation identifiers in a report unless they have been
irreversibly sanitized.

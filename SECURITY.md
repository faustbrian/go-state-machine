# Security policy

Security fixes are provided for the latest released major version and the Go
versions supported by that release.

Do not open a public issue for a suspected vulnerability. Use GitHub's private
security advisory flow for `faustbrian/state-machine`. Include an impact
summary, affected versions, reproduction, and suggested mitigation. You should
receive an acknowledgement within seven days.

Never include production state, event payloads, effect payloads, database
credentials, or correlation identifiers in a report unless they have been
irreversibly sanitized.

# Releasing

The active root source is planned v2 and release-blocked. Do not publish a v2
tag until the repository release gates pass, the v2 API and security behavior
are approved, and every owned consumer has been verified against a publicly
resolvable v2 candidate without local `replace` directives.

Released v1 remains the production line. Its API baseline must stay unchanged,
and README installation, release badges, general package reference, and owned
consumer dependencies must continue to resolve to released v1 until v2 is
published.

After publication, migrate consumers by changing imports to the `/v2` module,
reviewing error-string and persisted-error changes, configuring PostgreSQL
limits where needed, and running each consumer's own compatibility gates. Do
not claim rollback safety until the persisted-data overlap is verified.

# Releasing

The active root source is an unpublished v2 release candidate. Do not publish
a v2 tag until the repository release gates pass, the v2 API and security
behavior are approved, and release authority covers the selected source.

Publication and ecosystem adoption are separate boundaries. After publication,
verify the actual public v2 module without local `replace` directives and
complete affected owned-consumer adoption before claiming package security
closure. A publicly resolvable v2 is not a precondition for its first publication.

Released v1 remains the production line. Its API baseline must stay unchanged,
and README installation, release badges, general package reference, and owned
consumer dependencies must continue to resolve to released v1 until v2 is
published.

After publication, migrate consumers by changing imports to the `/v2` module,
reviewing error-string and persisted-error changes, configuring PostgreSQL
limits where needed, and running each consumer's own compatibility gates. Do
not claim rollback safety until the persisted-data overlap is verified.

Before publication, reconcile the dated major changelog and
[published-source security disposition](security/release-disposition.md) with
the exact selected source. Retain applicable runtime qualification when only
documentation changes; assess the final source's required CI separately.
After publication, record the actual fixed artifact and consumer result, then
coordinate any warranted advisory using the private reporting policy. Do not
describe an unpublished candidate or documentation-only follow-up as a new
security-fixed release.

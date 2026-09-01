# ADR-0003: Consent and private-by-default publication

- **Status:** Proposed
- **Context:** Automatic initialization and publication can expose proprietary
  code, personal data, credentials, or unfinished work. Repository visibility
  is a consequential decision separate from Git tracking.
- **Decision:** Require recorded tracking consent before mutation. The existing
  `.autogit` policy is normalized into a durable policy revision; `yes local`
  permits local commits but forbids remote calls, and hosted repositories are
  private by default. Require explicit, destination-specific public consent
  plus a pre-publication safety/quality summary. Never infer licensing.
- **Consequences:** First use has a small interaction cost and some workflows
  pause for public confirmation. Consent is idempotent and inspectable; policy
  changes invalidate dependent verification and publication readiness.
- **Alternatives considered:** Public by default improves immediate portfolio
  visibility but has unacceptable exposure risk. Inferring intent from a prompt
  is unreliable and vulnerable to prompt injection.

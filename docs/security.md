# Security

LangDeck can start, stop and reconfigure production agents, so it is built as a
security-sensitive system. This page describes the threat model, the controls in place,
and a checklist for production.

## Threat model (summary)

| Threat | Control |
|---|---|
| Unauthenticated access | Every API route except health and login requires a session or API key. |
| Default credentials | The seeded `admin` / `admin` account must choose a new password before it can do anything else; production refuses to disable that rule while the default is configured. Passwords are hashed with PBKDF2-SHA256 (600,000 iterations, random salt); login failures take the same time whether or not the user exists. |
| Stolen database backup | Session ids and API keys are stored only as SHA-256 hashes; the raw values never touch the database. |
| Cross-site request forgery | Cookie sessions require a per-session synchronizer token (`X-CSRF-Token`) on every state-changing request, plus `SameSite=Lax` cookies. |
| Session theft via XSS | Cookies are `HttpOnly`, `Secure` (over HTTPS) and use the `__Host-` prefix. A strict Content-Security-Policy forbids inline and third-party scripts. |
| Clickjacking | `X-Frame-Options: DENY` and `frame-ancestors 'none'`. |
| Privilege escalation | RBAC is enforced on the server for every route (viewer < operator < admin). The UI only hides what the server forbids. Admins cannot demote themselves. |
| Agent key misuse | Agent keys can only use the agent API, never the management API. Keys can be bound to a single agent name, can expire, and are revoked instantly. |
| Brute force / abuse | Per-IP rate limits on auth endpoints, per-credential limits on the API, request body size caps. |
| Login CSRF / code interception | OIDC Authorization Code flow with PKCE (S256), `state` and `nonce`, all compared in constant time; ID tokens are verified against the provider's keys. |
| Sensitive data in traces | The SDK redacts keys that look like secrets (`password`, `token`, `api_key`, `authorization`...), truncates large values, and can drop payloads entirely (`capture_payloads=False`). The server caps every payload at 64 KB. |
| Spoofed client IPs | `X-Forwarded-For` is ignored unless `LANGDECK_TRUST_PROXY_HEADERS=true`. |
| Metrics leakage | Prometheus metrics are served on a separate port, never through the public ingress. |
| Container escape / supply chain | Distroless, non-root, read-only root filesystem, all capabilities dropped, seccomp `RuntimeDefault`. Images are signed with cosign (keyless), ship SBOM and provenance, and are scanned with Trivy in CI. Dependencies are tracked by Dependabot and `govulncheck`. |
| Repudiation | Every change (runs, cancels, approvals, config, keys, roles, logins) is written to an append-only audit log with actor and IP. |

## Production checklist

- [ ] `LANGDECK_ENV=production` (refuses to start with dev auth, without a database, or without HTTPS).
- [ ] Prefer `LANGDECK_AUTH_MODE=oidc` with your identity provider. In `local` mode, sign in as
      `admin` / `admin` once and set a strong password immediately (LangDeck blocks every other
      action until you do), or provide `LANGDECK_ADMIN_PASSWORD` from a secret.
- [ ] With OIDC: Map groups to roles with
      `LANGDECK_OIDC_ADMIN_GROUPS` / `_OPERATOR_GROUPS` / `_VIEWER_GROUPS`.
- [ ] Restrict sign-in with `LANGDECK_ALLOWED_EMAIL_DOMAINS` if your IdP serves several organizations.
- [ ] Terminate TLS at your ingress; set `LANGDECK_PUBLIC_URL=https://...` and `LANGDECK_TRUST_PROXY_HEADERS=true`.
- [ ] Keep secrets (database URL, OIDC client secret) in a secret manager and mount them via `existingSecret`.
- [ ] Use a managed PostgreSQL with TLS (`sslmode=verify-full`) and backups.
- [ ] One agent key per agent, bound to the agent name, with an expiry. Rotate by creating a new key, deploying it, then revoking the old one.
- [ ] Enable the chart's `networkPolicy` so only your ingress (and agents) can reach LangDeck.
- [ ] Set `LANGDECK_RETENTION_DAYS` to match your data-retention policy.
- [ ] Review what your graphs put in state; use `capture_payloads=False` for agents handling regulated data.
- [ ] Verify image signatures before deploying:
      `cosign verify docker.io/rojinebrahimi/langdeck:<tag> --certificate-identity-regexp 'https://github.com/rojinebrahimi/langdeck/.*' --certificate-oidc-issuer https://token.actions.githubusercontent.com`

## Setting up OIDC

Register LangDeck as a confidential "web" client in your IdP:

- Redirect URI: `https://<your-langdeck>/auth/callback`
- Grant: authorization code (PKCE is always used)
- Scopes: `openid profile email` (add `groups` if your IdP needs it to emit group claims)

Examples:

| Provider | `LANGDECK_OIDC_ISSUER` | Groups |
|---|---|---|
| Keycloak | `https://kc.example.com/realms/<realm>` | add a "groups" mapper to the client scope |
| Okta | `https://<org>.okta.com` | add a `groups` claim to the ID token |
| Microsoft Entra ID | `https://login.microsoftonline.com/<tenant>/v2.0` | set `groupMembershipClaims`; values are group object ids |
| Google | `https://accounts.google.com` | no groups; use `LANGDECK_BOOTSTRAP_ADMINS` and assign roles in the UI |
| GitLab | `https://gitlab.com` | groups arrive in `groups` |
| Dex / Authentik / Zitadel | your issuer URL | standard `groups` claim |

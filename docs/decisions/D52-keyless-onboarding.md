# D52 — Onboarding without an API key

**Status:** Accepted (amends [D35](D35-accounts-onboarding-sessions-reset.md))

**Context:** D35 required an API key to create the first account, so a stranger reaching a fresh instance could not claim it. In practice that makes the first screen of the WebUI ask for a key the operator has to dig out of the server environment, which is poor UX for the person who just deployed it.

**Decision:**
- `POST /auth/onboard` no longer requires an API key. While no account exists, whoever reaches the instance first can create the first account and is signed in.
- It still refuses once any account exists (409), through the same single guarded `INSERT`.
- It requires the `X-M3m0s-Csrf` header, like login, so a cross-site form cannot claim the instance.
- API keys stay the credential for machines (hooks, MCP, CLI), and the key sign-in remains on the login screen.

**Trade-off:** until the first account exists, an instance reachable from the internet can be claimed by anyone, who then reads and writes all memory. Create the account right after deploying, or keep the instance private (VPN, reverse proxy with auth) until then.

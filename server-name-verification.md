# Server Name Verification for MCP Metaregistry

## Context and Problem Statement

The MCP Metaregistry will allow MCP server publishers to use domain-scoped namespaces for their server entries (e.g. 
`com.example/my-server`). We need a reliable way to ensure that a user claiming a domain-based namespace actually 
owns (or is authorized to use) that domain. In other words, if someone publishes a server under `com.github/*`, we 
must verify they control `github.com` to prevent impersonation or squatting. The solution should be secure, align 
with industry best practices, and manageable long-term.

## Decision Drivers

- __Security and Authenticity:__ Only legitimate domain owners should be able to publish under that domain's namespace.
  This prevents malicious actors from impersonating popular organizations.

- __Industry Best Practice:__ Favor solutions known to be secure and commonly used for domain ownership proof 
  (minimize inventing new untested methods).

- __Usability for Publishers:__ The verification process should be straightforward for developers and not require 
  excessive infrastructure (e.g., should work even if the domain isn't running a web server).

- __Continuous Trust:__ The mechanism should not only verify ownership once, but also detect if ownership changes (e.g.,
  domain expires or is sold) and revoke publishing rights if necessary to protect integrity.

- __Minimal External Dependencies:__ Rely primarily on the domain's DNS itself (which the owner already controls) 
  rather than third-party services, for simplicity and longevity.

- __Organizational Use:__ Enable both individual users and organizations to verify domains (so that a team/org can 
  publish under a corporate domain once verified).

- __Operational Maintainability:__ The solution should be possible to automate (for verification and periodic 
  re-checks) and monitor, with clear failure modes and recovery procedures.

## Considered Options

### Option 1: DNS TXT Record Verification

The user adds a specified TXT record to their domain's DNS zone to prove control. This is a widely adopted method 
(used by certificate authorities, cloud services, etc.) for domain ownership verification.

#### Pros

- __Highly secure__ requires direct access to domain DNS settings.
- __Independent__ of any web server, or HTTP content.
- __Industry-standard__ practice.
- DNS is __ubiquitous__.
- __Automate-able__ our service can query DNS anytime to validate. Continuous or repeated checks are straightforward 
  to implement by re-querying the DNS record.

#### Cons

- Requires the publisher to access and modify their DNS configuration, which may be non-intuitive for some users.
- DNS changes are __not instantaneous__. Propagation can take time (often minutes, sometimes hours), which could 
  delay verification.
- Without precautions, DNS lookups could be spoofed by an attacker (DNS poisoning) if not using secure resolvers or 
  DNSSEC.
- Keeping the TXT record in DNS long-term (for continuous verification) slightly "clutters" the domain's DNS zone, 
  though using a prefixed record minimizes any impact.

### Option 2: HTTP-01 Web Challenge

Provide a token that the user must serve via HTTP on a known URL (for example, hosting a file or response at 
`http://<domain>/.well-known/mcp-verification/<token>`). This approach is used by ACME (Let's Encrypt) for domain 
validation. 

#### Pros

- Fairly simple if the domain already hosts a website, the owner just drops a file or configures a response.
- Many developers are familiar with this from SSL certificate issuance.
- It doesn't require messing with DNS directly.
- Easy to automate if you have a webserver.
- Works with standard HTTP infrastructure.

#### Cons

- Not viable for domains that don't run an HTTP server or are not easily accessible on the internet.
- It fails for domains behind certain network restrictions (e.g., if port 80 is closed or filtered).
- Continuous monitoring would be complex. The registry would have to periodically re-fetch a URL and differentiate 
  between a temporarily down server vs. lost ownership.
- Introduces more points of failure (web hosting, redirects, etc.), whereas DNS is a more direct indicator of ownership.

### Option 3: DNS CAA or Certificate-Based Methods

Leverage the Certificate Authority Authorization (CAA) DNS record or possession of an SSL/TLS certificate for the 
domain as proof. CAA records specify which CAs can issue certificates for a domain (ussed in SSL issuance control). 
We could require a special CAA record or similar DNS record to prove ownership.

#### Pros

- If a domain owner can obtain a valid SSL certificate (which itself requires domain verification) or set a CAA, it 
  indirectly shows domain control.
- CAA is a DNS record, so it could be used in a similar way to TXT.
- Automatically checked by CAs, so some security-conscious domains already use it to restrict certificate issuance.

#### Cons

- CAA is not designed for arbitrary token storage or service-specific challenges. It only encodes which CAs are 
  allowed. Trying to repurpose it for our verification would be an abuse of its intended purpose and could conflict 
  with actual CAA usage.
- Not all domain owners set CAA, and those who do use it for security policies might be unwilling to change it for this.
- Using possession of an SSL certificate as proof is also problematic. It adds an extra step (obtaining a cert) and 
  still ultimately relies on DNS or email validation in the certificate process.

### Option 4: OAuth-Based Domain Linking (e.g. via GitHub)

Use a trusted third-party platform to vouch for domain ownership. For example, GitHub organizations allow domain 
verification (with DNS) to display a "Verified" badge. We could accept a link between the user's GitHub account/org 
and a domain as evidence, or use an OAuth flow with a provider that has the domain in email.

#### Pros

- In cases where the publisher is a company with an existing verified GitHub organization, this could save a step. 
  They may have already proven domain ownership to GitHub. Using an OAuth link or API, we might trust that 
  verification instead of asking for another DNS record.
- Offloads the verification to a known platform and might simplify the process for some users (no need to handle DNS 
  if already done elsewhere).

#### Cons

- Only works for a subset of users (e.g., those using GitHub and having verified domains there).
- It introduces an external dependency and potential single point of failure.
- Indirectly still using DNS for verification but are one step removed from the source of truth.

### Option 5: Email Confirmation to Domain Admin

Send a verification code via email to an address at the target domain (commonly used addresses like `admin@domain.com` 
or WHOIS contact). This method is sometimes offered by CAs for domain validation.

#### Pros

- Does not require DNS changes or hosting files.
- If the domain owner actively uses an administrative email, it's a direct way to reach them.
- It could be automated by sending an email and awaiting a confirmation link or code input.

#### Cons

- Assumes standard email aliases (`admin@`, `webmaster@`, etc.) or accurate WHOIS contact emails, which may not 
  exist or be monitored.
- Automating the ingestion of the confirmation is less straightforward (it may require a manual step by the user to 
  click a link or paste a code).
- Operationally, running an email service and handling bounces/non-delivery adds complexity.
- Many domains have WHOIS privacy.

## Proposed Solution

We support two __complementary__ ownership-verification mechanisms:

| Method                | Best for publishers who...                                             | Key strengths                                                                                                                                           | Key limitations                                                                                                                         |
|:----------------------|:-----------------------------------------------------------------------|:--------------------------------------------------------------------------------------------------------------------------------------------------------|:----------------------------------------------------------------------------------------------------------------------------------------|
| DNS TXT Record        | Can edit DNS at their registrar / DNS host                             | - Industry-standard proof of control<br>- Works even if the domain has no web server<br>- Easy to re-check automatically                                | - Requires DNS access<br>- Propagation delay                                                                                            |
| HTTP-01 Web Challenge | Already run a web site / can deploy a static file but cannot touch DNS | - No registrar access needed (just drop a file or route)<br>- Familiar to developers from Let's Encrypt<br>- Near-instant verification (no DNS caching) | - Fails if the domain has no publicly reachable HTTP(S) service<br>- Adds a second control plane (web hosting) that must stay available |

### Why offer both options?

- __Wider coverage = better UX__\
  Between DNS and HTTP we cover virtually all real-world setups. Examples: a SaaS team on a locked-down corporate 
  DNS can still verify via HTTP; a bare domain that hosts no site can verify by TXT.

- __Failsafe resilience__\
  If one control plain is down (DNS outage or web migration), the other can still validate (publish pipelines keep 
  moving).

- __Layered security__\
  For maintainers who enable _both_ methods of verification, an attacker must compromise both DNS and web origin to 
  hijack the namespace.

- __Consistent automation model__\
  Both rely on 128-bit random tokens and can be re-checked on every publish plus a nightly cron, so continuous trust 
  is preserved.

### How it works

1. __Token issuance:__ When a publisher first claims a custom domain namespace the registry generates a 128-bit 
   random token.

2. __Prove control via either path:__
   - __DNS path:__ Add TXT record `mcp-verify=<token>` to DNS.
   - __HTTP path:__ Host a plain-text file whose body is the token at `https://<domain>/.
   well-known/mcp-challenge/<token>`.

3. __Automated check:__ The CLI/server polls DNS or fetches the well-known URL; success in __either__ path marks the 
   domain verified for that user or organization.

4. __Continuous verification:__ To guard against later ownership changes, the registry re-checks __both__ indicators:
   - __Every publish__ immediately queries DNS and/or fetches the well-known file; publishing is allowed if at least 
   one token still matches.
   - __Background job (run on a regular cadence)__ re-checks every verified domain using both DNS and HTTP tokens. The 
   job will apply a failure-tolerance policy. For example, if a domain fails the check three times in a row, it is 
   marked unverified and new publishes are blocked. After the second consecutive failure, maintainers receive a 
   warning; if the check fails a third time, they are notified again as the domain status is downgraded. This guards 
   against transient outages while still revoking trust when ownership indicators consistently disappear.

This dual mechanism provides layered security, DNS is the gold-standard signal, while HTTP-01 offers a low-friction 
alternative for teams that cannot touch DNS. Together they:

- Cover nearly every hosting scenario (DNS-only, web-only, or both).

- Let maintainers migrate from one method to the other without renaming packages.

- Add resilience: if a DNS provider or web host is temporarily down, the other path still validates, keeping CI/CD 
  pipelines unblocked.

By combining DNS and HTTP verification, and by continuously validating whichever token(s) are configured, the MCP 
Metaregistry delivers high assurance of domain ownership while remaining flexible and developer-friendly.

### Positive Consequences

- High-confidence ownership with flexibility. DNS remains the gold-standard; HTTP-01 offers a low-friction 
  alternative when DNS edits are impossible.

- Reduced onboarding friction. Developers pick the path of least resistance; fewer support tickets.

- Operational robustness. Dual-path verification means fewer false blocks during provider outages.

- Organizational friendliness. Either method can be performed once by an infra team and thereafter reused by all 
  org members.

### Negative Consequences

- Slightly more code and monitoring. We must implement and observe two verification paths instead of one, and store 
  two tokens per domain.

- Extra edge-cases. Need clear rules for what happens if DNS passes but HTTP fails (and vice-versa). Policy: allow 
  publish if any passes; flag if both fail.

- Web-server dependency for HTTP-01. Projects choosing only HTTP must keep the well-known file reachable; transient 
  5xx outages could momentarily block publishes. Continuous checks mitigate but do not eliminate this risk.

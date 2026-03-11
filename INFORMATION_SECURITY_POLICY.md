# Information Security Policy

Last updated: 2026-03-09

This document describes the baseline information security controls for AspectOrbital's operator-run deployment.

## Scope

- Single-user, operator-managed deployment of AspectOrbital.
- Finance-spoke workflows that access financial data through Plaid.
- Supporting source control, infrastructure, and logging systems used to run the service.

Anyone who forks or self-hosts this project is responsible for implementing and enforcing their own security program.

## Security Owner

Mitchell Durbin  
Owner / Software Engineer  
dude0413@gmail.com

## Access Control

- Access to production systems is restricted to named administrative accounts.
- Least-privilege access is applied to infrastructure and service credentials.
- Shared credentials are avoided where possible.
- MFA is required for critical administrative accounts.

## Data Protection

- Data in transit is protected with TLS 1.2 or better. TLS termination is handled by a reverse proxy (e.g., nginx, Caddy, or Traefik) sitting in front of all Go services. Services must never be exposed directly to untrusted networks without TLS termination in place.
- Sensitive data at rest is encrypted on managed storage/services.
- Secrets (for example API keys and tokens) are stored outside source control.
- Private key files (`*.pem`, `*.key`, `*.p8`) are excluded from source control and must have filesystem permissions of `0600`.

## Secure Development

- Source code is maintained in GitHub with branch and review workflows.
- Dependency vulnerability and secret scanning are enabled where supported.
- Security updates are applied through routine patching.

## Monitoring and Vulnerability Management

- Dependency and secret alerts are reviewed on a regular basis.
- High-severity findings are prioritized for remediation.
- Runtime and error logs are used to detect abnormal behavior and failures.

## Incident Response

- Suspected security incidents are investigated promptly.
- Exposed or suspected compromised credentials are rotated immediately.
- Impacted integrations are disabled until risk is contained.

## Credential Rotation

### Plaid Access Tokens
Plaid access tokens represent persistent read access to linked bank accounts. They do not expire automatically.

Rotation procedure:
1. Log into the Plaid Dashboard and revoke the existing item.
2. Run the finance-spoke Plaid setup page (`/plaid/setup`) to re-link the bank account and obtain a new access token.
3. Update `PLAID_ACCESS_TOKENS` in the deployment `.env` and restart finance-spoke.
4. Verify the weekly summary runs successfully before considering rotation complete.

Trigger rotation if: the host is compromised, the `.env` file is exposed, or Plaid reports unauthorized access.

### Other Service Tokens
For `PLAID_CLIENT_ID` / `PLAID_SECRET`, `HUB_NOTIFY_AUTH_TOKEN`, `SPOKE_COMMAND_AUTH_TOKEN`, and third-party API keys:

1. Generate a new token/key in the respective dashboard.
2. Update the deployment `.env` and restart affected services.
3. Revoke the old token only after confirming the new one works.

## Retention and Privacy

- Data retention and deletion follow `DATA_RETENTION_POLICY.md`.
- Privacy handling follows `PRIVACY.md`.

## Review Cadence

- This policy is reviewed at least annually and after major architecture or provider changes.

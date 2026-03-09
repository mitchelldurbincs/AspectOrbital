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

- Data in transit is protected with TLS 1.2 or better.
- Sensitive data at rest is encrypted on managed storage/services.
- Secrets (for example API keys and tokens) are stored outside source control.

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

## Retention and Privacy

- Data retention and deletion follow `DATA_RETENTION_POLICY.md`.
- Privacy handling follows `PRIVACY.md`.

## Review Cadence

- This policy is reviewed at least annually and after major architecture or provider changes.

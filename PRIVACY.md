# Privacy Policy

Last updated: 2026-03-09

This Privacy Policy describes how AspectOrbital's finance-spoke collects, uses, stores, and deletes data when connected to financial institutions through Plaid.

## Scope

AspectOrbital is a personal, limited-scope application operated by Mitchell Durbin. It is not a public consumer application.

- This policy applies to the operator-run deployment of AspectOrbital.
- The public GitHub repository contains source code and documentation, not production financial data.
- Anyone who forks or self-hosts this project is responsible for their own data practices as an independent controller/operator.

## Data Controller and Contact

Mitchell Durbin  
Owner / Software Engineer  
dude0413@gmail.com

## Data We Collect

- Plaid-provided account and transaction data needed for subscription detection and summaries.
- Plaid Item metadata and access credentials (tokens), handled as secrets.
- Operational metadata such as timestamps, job status, and error logs.

## How We Use Data

- To connect authorized financial accounts through Plaid.
- To generate recurring subscription summaries and finance-related insights.
- To monitor, troubleshoot, and secure the service.

## Consent

This deployment is single-user. The operator provides explicit consent by initiating account connection through Plaid and can revoke consent by disconnecting linked accounts and requesting deletion.

## Sharing

- Data is shared only with processors required to deliver the service (for example, Plaid and hosting/infrastructure providers).
- We do not sell personal data.
- We do not share data for advertising.

## Data Retention and Deletion

- Financial data is retained only for the minimum period needed to operate the service.
- When an account is disconnected or a deletion request is received, associated data is deleted within 30 days.
- See `DATA_RETENTION_POLICY.md` for detailed retention rules.

## Security

We use administrative, technical, and physical safeguards appropriate to the sensitivity of financial data, including:

- TLS 1.2+ for data in transit.
- Encryption at rest on managed systems.
- MFA for critical administrative accounts.
- Least-privilege access controls.
- Secret management outside source control and vulnerability monitoring.

## Your Rights

Because this deployment is single-user, data rights requests map to the operator account. For any data access, correction, or deletion request, contact dude0413@gmail.com.

## Changes

We may update this policy periodically. Updated versions will be posted in this repository with a revised "Last updated" date.

# Data Retention and Deletion Policy

Last updated: 2026-03-09

This policy defines how AspectOrbital's finance-spoke retains and deletes data received from Plaid and related systems.

## Scope

This policy applies to financial data, metadata, credentials, and logs processed by the finance-spoke service.

This policy covers the operator-run deployment only. Anyone who forks or self-hosts this project is responsible for defining and enforcing their own retention and deletion policy.

## Data Classification and Retention

1. Plaid access credentials (Item access tokens)
   - Retained while the corresponding financial connection is active.
   - Deleted within 30 days after account disconnect or deletion request.

2. Account and transaction data retrieved from Plaid
   - Retained only as needed to generate subscription summaries and finance outputs.
   - Deleted within 30 days after account disconnect or deletion request.

3. Derived outputs (subscription summaries, alerts, aggregate metrics)
   - Retained up to 90 days for troubleshooting and continuity unless earlier deletion is requested.
   - Deleted within 30 days after request if tied to a disconnected account.

4. Operational and security logs
   - Retained up to 30 days by default.
   - Retained up to 90 days when needed for incident investigation or abuse prevention.

5. Backups
   - Backups follow the same retention intent and are rotated out within 30 days where technically feasible.

## Deletion Triggers

Deletion is initiated when any of the following occurs:

- A linked financial account is disconnected.
- A data deletion request is received at dude0413@gmail.com.
- Data exceeds its retention period.

## Deletion Process

- Production records are removed or anonymized.
- Related secrets and tokens are revoked and deleted.
- Backup data ages out through backup rotation.
- Deletion actions are logged for auditability.

## Enforcement and Review

- Access to retained data is limited to authorized administrative accounts.
- MFA is required for critical systems handling retained financial data.
- This policy is reviewed at least annually and after major architecture changes.

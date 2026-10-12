# ADR-0004: Secrets — Envelope Encryption + SecretStore

- **Status:** Accepted
- **Date:** 2026-10-12

## Context
SEMAR stores credentials for every cloud. A database leak must not directly leak those credentials. Vault is not available yet and would add operational load.

## Decision
- A `SecretStore` interface whose first implementation is **envelope encryption** in PostgreSQL: one DEK per secret (AES-256-GCM, AAD = `table|row_id|purpose`), with the DEK wrapped by a master key (KEK).
- Every row stores `kek_version` from Phase 1.
- The KEK is kept **outside the database** (KMS/Vault, or at minimum a different host/namespace from PostgreSQL in production).

## Consequences
- KEK rotation = rewrapping DEKs online, without re-encrypting the data.
- A Vault implementation can be added behind `SecretStore` without changing callers.
- Backups must include the KEK separately from the database; a rotation runbook is required.

## Alternatives
- **Vault from day one:** strongest option, but adds a Tier-0 component before the MVP.
- **Column encryption with one static key:** simple, but rotation means re-encrypting all data.

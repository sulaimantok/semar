# Runbook: rotating keys and provider credentials

SEMAR holds three kinds of secrets. Each rotates without downtime by adding the new value first and removing the old one only when nothing depends on it.

| Secret | Variables | Protects | Held by |
|---|---|---|---|
| Payload key | `SEMAR_PAYLOAD_KEY_ID`, `SEMAR_PAYLOAD_KEY`, `SEMAR_PAYLOAD_PREVIOUS_KEYS` | Workflow inputs and results stored in Temporal | semar-worker (and anything that reads histories) |
| KEK | `SEMAR_KEK_ID`, `SEMAR_KEK`, `SEMAR_KEK_PREVIOUS` | Provider credentials in PostgreSQL (envelope encryption, ADR-0004) | semar-api, semar-worker |
| Provider credentials | stored through the API | Access to OpenStack / Proxmox | PostgreSQL, sealed with the KEK |

Generate a key with `openssl rand -base64 32`. Keep keys outside the database host, and back them up separately from database backups.

## Payload key

1. Set a new `SEMAR_PAYLOAD_KEY_ID` (e.g. `k2`) and `SEMAR_PAYLOAD_KEY`; move the old pair into `SEMAR_PAYLOAD_PREVIOUS_KEYS` (`k1=<old key>`).
2. Restart every semar-worker. On start each worker rewrites its sync schedules, which re-encrypts them with the new key.
3. Keep the old key until every workflow history written with it has passed the namespace retention (72 h in the Lite profile), then remove it from `SEMAR_PAYLOAD_PREVIOUS_KEYS` and restart again.

Removing the old key too early makes old histories unreadable; it never affects the inventory in PostgreSQL.

## KEK

1. Set a new `SEMAR_KEK_ID` and `SEMAR_KEK`; move the old pair into `SEMAR_KEK_PREVIOUS` (`k1=<old key>`). Apply the **same** values to semar-api and semar-worker.
2. Restart both. New and replaced credentials are sealed with the new KEK; existing ones still open with the old one.
3. Rewrap every credential (only the data keys are re-encrypted, never the secrets themselves):

   ```bash
   semar-api kek-rewrap
   ```

   It fails if any credential is still on an old KEK (for example because it was being written concurrently); run it again until it succeeds.
4. Remove the old KEK from `SEMAR_KEK_PREVIOUS` on both and restart.

## Provider credentials

Rotate at the provider first, then in SEMAR, so there is never a moment without a valid credential:

1. Create the new credential at the provider (a new OpenStack application credential, or a new Proxmox API token with the same role; see the README's *Provider credentials*).
2. Store it in SEMAR with the same purpose and scope (**Settings → Providers → provider → Store or replace a credential**, or `PUT /api/v1/providers/{id}/credentials`). Storing with the same purpose and scope replaces the old one; the worker picks it up on the next sync.
3. Run **Check connection** and wait for one healthy sync.
4. Revoke the old credential at the provider.

Every store, replacement and deletion is in the audit log; secrets never are.

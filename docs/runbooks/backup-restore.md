# Runbook: backup and restore

A restorable SEMAR needs three things, backed up separately:

| What | Why | How |
|---|---|---|
| SEMAR's PostgreSQL database | Settings, users, audit log, sealed credentials, inventory cache | `pg_dump` or continuous archiving (WAL) |
| Keys: KEK(s) and payload key(s) | Without the KEK the sealed credentials are unrecoverable; without the payload keys Temporal histories are unreadable | Your secret store; never in the same backup as the database |
| Temporal's database | Schedules and in-flight workflow state | Temporal's own persistence backup; optional for the read-only Phase 1 |

The inventory cache can always be rebuilt from the providers, and sync schedules are recreated by the workers from the database. Losing Temporal's state in Phase 1 loses only in-flight syncs.

## Back up

```bash
pg_dump --format=custom --file=semar-$(date -u +%Y%m%dT%H%M%SZ).dump "$SEMAR_OWNER_DATABASE_URL"
```

Use the owner role (docs/runbooks/database-roles.md) so the dump includes ownership and grants. Encrypt the dump at rest and keep it apart from the keys.

## Restore

1. Create the roles and an empty database as in the database roles runbook.
2. Restore as the owner:

   ```bash
   pg_restore --no-owner --role=semar_owner --dbname="$SEMAR_OWNER_DATABASE_URL" semar-<timestamp>.dump
   ```

3. Re-apply the grants for the app role and the audit log revoke from the database roles runbook, then run `semar-api migrate` (as the owner) in case the backup predates the running version.
4. Start semar-api and semar-worker with the **same KEK and payload keys** that were current when the backup was taken (plus any newer ones).
5. Verify:
   - `semar-api audit-verify` reports the chain as OK;
   - **Check connection** succeeds for a real provider (proves the KEK opens the credentials);
   - one sync per provider becomes healthy.

If **Check connection** answers `503 … cannot be decrypted with the configured SEMAR_KEK`, the services were started with a KEK that did not seal the credentials. Start them with the KEK(s) from the time of the backup; nothing in the database needs to change.

This procedure, from `pg_dump` as the owner through restore, grants, `semar-api migrate`, `audit-verify` and a successful connection check on the restored database, has been exercised end to end against PostgreSQL 16.

## Test the restore

Restore into a scratch database on a schedule (at least before each release) and run the verification above. A backup that has not been restored is not a backup.

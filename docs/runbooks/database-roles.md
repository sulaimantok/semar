# Runbook: database roles

SEMAR's audit log is append-only because triggers reject `UPDATE` and `DELETE` on `audit_events`, and each event is hash-chained to the previous one. A **superuser or the owner of the table** can disable those triggers. In production, semar-api and semar-worker must therefore connect as a role that is neither a superuser nor the owner of any SEMAR table; only migrations run as the owner.

| Role | Used by | Can |
|---|---|---|
| `semar_owner` | `semar-api migrate` only | Own the schema and tables, run DDL |
| `semar_app` | semar-api, semar-worker | Read and write rows; append to the audit log; no DDL, no `TRUNCATE` |

## One-time setup

Run as a PostgreSQL superuser. Choose your own passwords and keep them in your secret store.

```sql
CREATE ROLE semar_owner LOGIN PASSWORD '<owner password>';
CREATE ROLE semar_app   LOGIN PASSWORD '<app password>' NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION;
CREATE DATABASE semar OWNER semar_owner;
\c semar
ALTER SCHEMA public OWNER TO semar_owner;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO semar_app;
-- Tables and sequences created later by migrations are usable by the app.
ALTER DEFAULT PRIVILEGES FOR ROLE semar_owner IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO semar_app;
ALTER DEFAULT PRIVILEGES FOR ROLE semar_owner IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO semar_app;
```

## Every upgrade

1. Apply migrations as the owner:

   ```bash
   SEMAR_DATABASE_URL='postgres://semar_owner:<owner password>@db.example.com:5432/semar?sslmode=verify-full' \
     semar-api migrate
   ```

2. Take the audit log out of the app's reach beyond appending (the triggers already refuse it; this makes the refusal a permission error too):

   ```sql
   REVOKE UPDATE, DELETE, TRUNCATE ON audit_events FROM semar_app;
   ```

3. Run semar-api and semar-worker with the app role and without startup migrations:

   ```bash
   SEMAR_DATABASE_URL='postgres://semar_app:<app password>@db.example.com:5432/semar?sslmode=verify-full'
   SEMAR_MIGRATE_ON_START=false
   ```

## Check

As `semar_app`, each of these must fail with `permission denied` or a trigger error:

```sql
ALTER TABLE audit_events DISABLE TRIGGER ALL;
UPDATE audit_events SET actor = 'x';
TRUNCATE audit_events;
CREATE TABLE probe (id int);
```

Then `semar-api audit-verify` (as `semar_app`) must report the chain as OK.

The Lite profile (`deploy/compose/`) uses a single role for convenience and is not covered by this guarantee.

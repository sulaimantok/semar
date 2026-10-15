# Runbook: break-glass access

Until OIDC arrives (Phase 2), SEMAR has local accounts only. A local global admin stays available afterwards as the break-glass path for when the identity provider is down.

## Keep a break-glass account

- The `admin` account created by `SEMAR_BOOTSTRAP_ADMIN_PASSWORD` is the break-glass account. Remove the variable after the first start.
- Store its password in your secret store with dual control; do not use it for daily work. Create named accounts for people.
- Review the audit log for its use: every login, logout and change records the actor.

## Locked out (lost password, disabled account)

Run on a host that can reach the database, with the same `SEMAR_DATABASE_URL` as semar-api (the app role is enough):

```bash
semar-api reset-password admin
# type the new password (at least 12 characters), then Enter
```

The password is read from stdin only, never from arguments or the environment. The command sets the new password, re-enables the account, ends all its sessions and writes `user.reset_password` by `break-glass-cli` to the audit log. Afterwards:

1. Sign in with the new password and confirm the expected access.
2. Run `semar-api audit-verify` and check the audit log for activity you do not recognise.
3. Rotate the password again into the secret store once the incident is over.

## Too many attempts

Login attempts are rate limited per client address and per username in each semar-api process (roadmap risk R17). A locked username unlocks by itself after the window (15 minutes by default); restarting semar-api clears the counters immediately.

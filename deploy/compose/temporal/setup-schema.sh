#!/bin/sh
# Creates and migrates the Temporal databases. Safe to re-run: creation steps
# tolerate existing objects and update-schema only applies missing versions.
set -eu
SCHEMA=/etc/temporal/schema/postgresql/v12
sql() { temporal-sql-tool --plugin postgres12 --ep "$POSTGRES_SEEDS" -p 5432 -u "$POSTGRES_USER" -pw "$POSTGRES_PWD" "$@"; }

for db in temporal temporal_visibility; do
  sql --db "$db" create-database 2>/dev/null || echo "database $db already exists"
done
sql --db temporal setup-schema -v 0.0 2>/dev/null || echo "temporal schema already initialised"
sql --db temporal update-schema -d "$SCHEMA/temporal/versioned"
sql --db temporal_visibility setup-schema -v 0.0 2>/dev/null || echo "visibility schema already initialised"
sql --db temporal_visibility update-schema -d "$SCHEMA/visibility/versioned"
echo "schema up to date"

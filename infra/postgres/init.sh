#!/usr/bin/env bash
# Superuser bootstrap for the service roles, schemas and extensions; run it before any migration.
# Idempotent and independent of the database name, so the same script serves the
# local Compose/Testcontainers init and the one-time Railway bootstrap (Unplayd Operations runbook in Linear).
set -euo pipefail
export USER_API_DB_PASSWORD="${USER_API_DB_PASSWORD:?required}" CATALOG_DB_PASSWORD="${CATALOG_DB_PASSWORD:?required}"
psql --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" --no-psqlrc --set=ON_ERROR_STOP=1 <<'SQL'
\getenv user_password USER_API_DB_PASSWORD
\getenv catalog_password CATALOG_DB_PASSWORD
BEGIN;
SELECT format('CREATE ROLE %I LOGIN', role)
FROM unnest(ARRAY['user_api', 'catalog']) AS role
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = role) \gexec
ALTER ROLE user_api WITH LOGIN PASSWORD :'user_password';
ALTER ROLE catalog WITH LOGIN PASSWORD :'catalog_password';
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
SELECT format('REVOKE ALL ON DATABASE %I FROM PUBLIC', current_database()) \gexec
SELECT format('GRANT CONNECT ON DATABASE %I TO user_api, catalog', current_database()) \gexec
CREATE SCHEMA IF NOT EXISTS user_data AUTHORIZATION user_api;
CREATE SCHEMA IF NOT EXISTS catalog AUTHORIZATION catalog;
REVOKE ALL ON SCHEMA user_data, catalog FROM PUBLIC;
ALTER ROLE user_api SET search_path = user_data;
ALTER ROLE catalog SET search_path = catalog;
-- Only superusers may create extensions here; the catalog title search index needs trigrams.
CREATE EXTENSION IF NOT EXISTS pg_trgm SCHEMA catalog;
COMMIT;
SQL

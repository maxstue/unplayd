#!/usr/bin/env bash
set -euo pipefail
psql --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" --set=ON_ERROR_STOP=1 \
  --set=user_password="$USER_API_DB_PASSWORD" --set=catalog_password="$CATALOG_DB_PASSWORD" <<'SQL'
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON DATABASE unplayd FROM PUBLIC;
CREATE ROLE user_api LOGIN PASSWORD :'user_password';
CREATE ROLE catalog LOGIN PASSWORD :'catalog_password';
GRANT CONNECT ON DATABASE unplayd TO user_api, catalog;
CREATE SCHEMA user_data AUTHORIZATION user_api;
CREATE SCHEMA catalog AUTHORIZATION catalog;
REVOKE ALL ON SCHEMA user_data, catalog FROM PUBLIC;
ALTER ROLE user_api SET search_path = user_data;
ALTER ROLE catalog SET search_path = catalog;
SQL

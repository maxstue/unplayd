#!/usr/bin/env bash
# Idempotent Railway database bootstrap. Run it once per environment before the first
# deployment of user-api and catalog-api, and again whenever infra/postgres/init.sh changes.
#
# Runs infra/postgres/init.sh as superuser inside the postgres service over `railway ssh`, which
# creates the service roles, schemas and the pg_trgm extension in the catalog schema. The role
# passwords are the DATABASE_PASSWORD variables of user-api and catalog-api; the script only reads
# them. For a new environment, set them first, e.g. for each of both services:
#   openssl rand -hex 32 | railway variable set --service user-api --stdin --skip-deploys DATABASE_PASSWORD
#
# Database passwords are sent over SSH on stdin and passed to psql in its environment.
# `railway ssh` authenticates with the workspace SSH key from SSH_KEY_PRIVATE when it is set
# (Infisical prod: /External_Services/Railway), otherwise with the keys in ~/.ssh.
# Usage: vp run railway:bootstrap-db  or  infra/railway/bootstrap-db.sh [environment] (default: production)
set -euo pipefail
cd "$(dirname "$0")/../.."
environment=${1:-production}

if ! railway tcp-proxy list --service postgres --environment "$environment" --json |
  jq -e '.proxies | type == "array" and length == 0' >/dev/null; then
  echo "Could not verify that postgres has no public TCP proxy in $environment. Bootstrap aborted." >&2
  exit 1
fi

ssh_args=()
if [[ -n ${SSH_KEY_PRIVATE:-} ]]; then
  key_dir=$(mktemp -d)
  trap 'rm -rf "$key_dir"' EXIT
  (umask 077 && printf '%s\n' "$SSH_KEY_PRIVATE" >"$key_dir/id")
  ssh_args=(--identity-file "$key_dir/id")
fi

read_password() {
  local service=$1 value
  if ! value=$(railway variable list --service "$service" --environment "$environment" --json |
    jq -er '.DATABASE_PASSWORD | select(type == "string" and length > 0)'); then
    echo "DATABASE_PASSWORD of $service is missing, empty or unreadable in $environment; set it first." >&2
    return 1
  fi
  printf %s "$value"
}

user_password=$(read_password user-api)
catalog_password=$(read_password catalog-api)

{
  printf 'export USER_API_DB_PASSWORD=%q CATALOG_DB_PASSWORD=%q\n' "$user_password" "$catalog_password"
  cat infra/postgres/init.sh
} | railway ssh --service postgres --environment "$environment" ${ssh_args[@]+"${ssh_args[@]}"} -- bash -s
echo "Database bootstrap complete for $environment." >&2

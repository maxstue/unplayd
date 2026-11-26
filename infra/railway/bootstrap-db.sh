#!/usr/bin/env bash
# Idempotent Railway database bootstrap. Run it once per environment before the first
# deployment of user-api and catalog-api, and again whenever infra/postgres/init.sh changes.
#
# 1. Generates DATABASE_PASSWORD on user-api and catalog-api when it is missing.
# 2. Runs infra/postgres/init.sh as superuser inside the postgres service over `railway ssh`,
#    which creates the service roles, schemas and the pg_trgm extension in the catalog schema.
#
# Database secrets only travel through stdin; they never appear in arguments, files or output.
# `railway ssh` authenticates with the workspace SSH key from SSH_KEY_PRIVATE when it is set
# (Infisical prod: /External_Services/Railway), otherwise with the keys in ~/.ssh.
# Usage: vp run railway:bootstrap-db  or  infra/railway/bootstrap-db.sh [environment] (default: production)
set -euo pipefail
cd "$(dirname "$0")/../.."
environment=${1:-production}

ssh_args=()
if [[ -n ${SSH_KEY_PRIVATE:-} ]]; then
  key_dir=$(mktemp -d)
  trap 'rm -rf "$key_dir"' EXIT
  (umask 077 && printf '%s\n' "$SSH_KEY_PRIVATE" >"$key_dir/id")
  ssh_args=(--identity-file "$key_dir/id")
fi

password_for() {
  local service=$1 value
  value=$(railway variable list --service "$service" --environment "$environment" --json | jq -r '.DATABASE_PASSWORD // empty')
  if [[ -z $value ]]; then
    value=$(openssl rand -hex 32)
    printf %s "$value" | railway variable set --service "$service" --environment "$environment" \
      --stdin --skip-deploys DATABASE_PASSWORD >/dev/null
    echo "Generated DATABASE_PASSWORD for $service." >&2
  fi
  printf %s "$value"
}

user_password=$(password_for user-api)
catalog_password=$(password_for catalog-api)

{
  printf 'export USER_API_DB_PASSWORD=%q CATALOG_DB_PASSWORD=%q\n' "$user_password" "$catalog_password"
  cat infra/postgres/init.sh
} | railway ssh --service postgres --environment "$environment" ${ssh_args[@]+"${ssh_args[@]}"} -- bash -s
echo "Database bootstrap complete for $environment." >&2

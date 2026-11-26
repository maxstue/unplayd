#!/usr/bin/env bash
# Copies the service secrets from Infisical into Railway variables. Infisical is the source;
# rerun after changing a secret there. Non-secret settings (URLs, ports) live in .railway/railway.ts,
# which declares these variables with preserve(). DATABASE_PASSWORD is owned by bootstrap-db.sh.
#
# Infisical folder (environment prod by default) -> Railway service:
#   /Web -> web, /User_API -> user-api, /Catalog_API -> catalog-api
# Values only travel through stdin; they never appear in arguments, files or output.
# Usage: vp run railway:sync-secrets  or  infra/railway/sync-secrets.sh [railway-env] [infisical-env]
set -euo pipefail
cd "$(dirname "$0")/../.."
railway_environment=${1:-production}
infisical_environment=${2:-prod}

sync() {
  local folder=$1 service=$2 secrets key
  secrets=$(infisical secrets --env "$infisical_environment" --path "$folder" -o json --silent)
  while IFS= read -r key; do
    jq -j --arg key "$key" '.[] | select(.secretKey == $key) | .secretValue' <<<"$secrets" |
      railway variable set --service "$service" --environment "$railway_environment" \
        --stdin --skip-deploys "$key" >/dev/null
    echo "Synced $service $key" >&2
  done < <(jq -r '.[].secretKey' <<<"$secrets")
}

sync /Web web
sync /User_API user-api
sync /Catalog_API catalog-api
echo "Secrets synced to $railway_environment; redeploy the services to apply them." >&2

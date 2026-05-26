#!/bin/bash
#
# Render GCP Deployment Manager config with a custom prefix.
# Replaces all occurrences of the default prefix 'nextgen-s3' with the user-provided one.
#
# Usage:
#   render_gcp_config.sh <prefix>
#
# Example:
#   render_gcp_config.sh alice-test > /tmp/alice-test-deployment.yaml
#   gcloud deployment-manager deployments create alice-test-cluster \
#     --config /tmp/alice-test-deployment.yaml

set -e

DEFAULT_PREFIX="nextgen-s3"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TEMPLATE="${SCRIPT_DIR}/gcp-deployment-config.yaml"

if [[ ! -f "$TEMPLATE" ]]; then
    echo "Error: Template not found at $TEMPLATE" >&2
    exit 1
fi

if [[ -z "$1" ]]; then
    echo "Usage: $0 <prefix>" >&2
    echo "  Renders the GCP deployment config with <prefix> substituted for '${DEFAULT_PREFIX}'." >&2
    echo "  Output goes to stdout; redirect to a file to save." >&2
    echo "" >&2
    echo "Example:" >&2
    echo "  $0 alice-test > /tmp/alice-test-deployment.yaml" >&2
    exit 1
fi

PREFIX="$1"

# Validate prefix: alphanumeric + hyphen only, max 20 chars, must not start/end with hyphen
if [[ ! "$PREFIX" =~ ^[a-zA-Z0-9]([a-zA-Z0-9-]{0,18}[a-zA-Z0-9])?$ ]]; then
    echo "Error: Prefix must be 1-20 chars, alphanumeric or hyphens, and cannot start/end with a hyphen." >&2
    exit 1
fi

sed "s/${DEFAULT_PREFIX}/${PREFIX}/g" "$TEMPLATE"

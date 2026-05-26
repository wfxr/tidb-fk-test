#!/bin/bash
#
# Generate a short GCP resource prefix and verify it is not currently used.
# The final uniqueness guard is still `gcloud deployment-manager deployments create`,
# which fails atomically if the deployment name already exists.

set -e

usage() {
    echo "Usage: $0 [base-name]" >&2
    echo "  Prints an available prefix to stdout." >&2
    echo "  Optional base-name defaults to USER, then tidbx." >&2
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    usage
    exit 0
fi

sanitize_base() {
    local raw=${1:-}
    local cleaned
    cleaned=$(echo "$raw" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9-]+/-/g; s/^-+//; s/-+$//; s/-+/-/g')
    if [[ -z "$cleaned" ]]; then
        cleaned="tidbx"
    fi
    echo "${cleaned:0:6}" | sed -E 's/-+$//'
}

validate_prefix() {
    local prefix=$1
    [[ "$prefix" =~ ^[a-zA-Z0-9]([a-zA-Z0-9-]{0,18}[a-zA-Z0-9])?$ ]]
}

prefix_in_use() {
    local prefix=$1
    local deployment="${prefix}-cluster"

    if gcloud deployment-manager deployments describe "$deployment" >/dev/null 2>&1; then
        return 0
    fi

    if [[ -n "$(gcloud compute instances list --filter="name~'^${prefix}-'" --format='value(name)' 2>/dev/null | head -1)" ]]; then
        return 0
    fi

    if [[ -n "$(gcloud compute disks list --filter="name~'^${prefix}-'" --format='value(name)' 2>/dev/null | head -1)" ]]; then
        return 0
    fi

    return 1
}

base=$(sanitize_base "${1:-${USER:-tidbx}}")

for _ in $(seq 1 20); do
    suffix="$(date -u +%m%d%H%M)-$(LC_ALL=C tr -dc 'a-f0-9' </dev/urandom | head -c 4)"
    prefix="${base}-${suffix}"

    if ! validate_prefix "$prefix"; then
        echo "Internal error: generated invalid prefix: $prefix" >&2
        exit 1
    fi

    echo "Checking prefix candidate: $prefix" >&2
    if ! prefix_in_use "$prefix"; then
        echo "$prefix"
        exit 0
    fi

done

echo "Error: failed to find an unused prefix after 20 attempts" >&2
exit 1

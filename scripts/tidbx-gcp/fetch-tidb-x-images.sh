#!/bin/bash
#
# Fetch TiDB X binaries from private registry images or OCI artifacts and package them for tiup cluster patch.
# Auto-routes between image layer extraction and direct artifact download based on input.
#
# Usage:
#   fetch_tidb_x_images.sh --component <tidb|tikv|pd> --tag <tag> [--output <dir>]
#   fetch_tidb_x_images.sh --component <tidb|tikv|pd> --image <image-ref> [--output <dir>]
#   fetch_tidb_x_images.sh --all [--tag <tag>] [--output <dir>]
#
# Examples:
#   fetch_tidb_x_images.sh --component tikv --tag cloud-engine-nextgen --output /tmp
#   fetch_tidb_x_images.sh --component tidb --tag master-nextgen_linux_amd64 --output /tmp
#   fetch_tidb_x_images.sh --all --output /tmp
#   fetch_tidb_x_images.sh --component tikv --image us-docker.pkg.dev/pingcap-testing-account/dev/tikv/tikv/image:my-tag --output /tmp
#

set -e

REGISTRY="us.gcr.io/pingcap-public/tidbx"
ARTIFACT_REGISTRY="us-docker.pkg.dev/pingcap-testing-account/tidbx/pingcap"
PLATFORM="linux/amd64"

usage() {
    echo "Usage: $0 --component <tidb|tikv|pd> --tag <tag> [--output <dir>]"
    echo "       $0 --component <tidb|tikv|pd> --image <image-ref> [--output <dir>]"
    echo "       $0 --all [--tag <tag>] [--output <dir>]"
    echo ""
    echo "Options:"
    echo "  -c, --component    Component to fetch: tidb, tikv, or pd (requires --tag)"
    echo "  -t, --tag          Image/artifact tag (required for --component; optional for --all)"
    echo "  -i, --image        Full image or artifact reference for one component (overrides default registry/tag)"
    echo "  -o, --output       Output directory for tarballs (default: /tmp)"
    echo "  -a, --all          Fetch all common components (tidb, tikv, pd)"
    echo "      --list-tags    List available tags and exit (non-interactive)"
    echo "  -h, --help         Show this help"
    echo ""
    echo "Note: tikv component produces tikv.tar.gz which contains both tikv-server and tikv-worker"
    exit 1
}

# Ensure oras and jq are available
check_prerequisites() {
    if ! command -v oras &> /dev/null; then
        echo "Error: oras is not installed. Please install oras first."
        echo "  https://oras.land/docs/installation"
        exit 1
    fi
    if ! command -v jq &> /dev/null; then
        echo "Error: jq is not installed. Please install jq first."
        exit 1
    fi
}

# Extract registry host from an image repo or image reference.
registry_host_for() {
    local ref=$1
    echo "${ref%%/*}"
}

# Default artifact repo for a component
artifact_repo_for() {
    local component=$1
    echo "$ARTIFACT_REGISTRY/$component/package"
}

# Detect fetch mode from repo path and/or tag.
# Priority: repo path > tag suffix > fallback default
# Fallback default is "artifact" because it is the more efficient path.
detect_mode() {
    local repo="${1:-}"
    local tag="${2:-}"

    if [[ -n "$repo" ]]; then
        if [[ "$repo" == */package ]]; then
            echo "artifact"
        else
            echo "image"
        fi
    elif [[ -n "$tag" && "$tag" =~ _linux_(amd64|arm64)$ ]]; then
        echo "artifact"
    elif [[ -n "$tag" ]]; then
        echo "image"
    else
        echo "artifact"
    fi
}

# Split image reference into repo and tag.
parse_image_ref() {
    local image_ref=$1
    local __repo_var=$2
    local __tag_var=$3

    if [[ "$image_ref" == *@sha256:* ]]; then
        echo "Error: digest image references are not supported yet; use a tag reference"
        return 1
    fi
    local last_path_part="${image_ref##*/}"
    if [[ "$image_ref" != */* || "$last_path_part" != *:* ]]; then
        echo "Error: --image must be a full image reference with registry/repo:tag"
        return 1
    fi

    local repo="${image_ref%:*}"
    local tag="${image_ref##*:}"
    if [[ -z "$repo" || -z "$tag" || "$repo" == "$tag" ]]; then
        echo "Error: --image must include a non-empty repo and tag"
        return 1
    fi

    printf -v "$__repo_var" '%s' "$repo"
    printf -v "$__tag_var" '%s' "$tag"
}

# Ensure logged in to the target registry (test auth against a known repo/tag)
check_gcr_auth() {
    local test_repo="${1:-$REGISTRY/tikv}"
    local test_tag="${2:-cloud-engine-nextgen}"
    local registry_host
    registry_host=$(registry_host_for "$test_repo")
    if ! oras resolve "$test_repo:$test_tag" &> /dev/null; then
        echo "Logging into $registry_host with gcloud token..."
        local token
        if ! token=$(gcloud auth print-access-token 2>/dev/null); then
            echo "Error: Failed to get gcloud access token."
            echo "Make sure you are authenticated with gcloud:"
            echo "  gcloud auth login"
            exit 1
        fi
        if ! echo "$token" | oras login -u oauth2accesstoken --password-stdin "$registry_host" &> /dev/null; then
            echo "Error: Failed to login to $registry_host"
            echo "Make sure gcloud is authenticated and has access to the registry."
            exit 1
        fi
        echo "Login succeeded."
    fi
}

# Default tags per component (team conventions)
default_tag_for() {
    local component=$1
    local mode="${2:-artifact}"
    case "$component" in
        tidb|pd)
            if [[ "$mode" == "artifact" ]]; then
                echo "master-nextgen_linux_amd64"
            else
                echo "master-nextgen"
            fi
            ;;
        tikv)
            if [[ "$mode" == "artifact" ]]; then
                echo "cloud-engine-nextgen_linux_amd64"
            else
                echo "cloud-engine-nextgen"
            fi
            ;;
        *) echo "" ;;
    esac
}

# Print available tags and recommended default, then exit
list_tags() {
    local component=$1
    local repo="$2"
    local default_tag="$3"

    echo "Fetching recent tags for $repo..."
    local tags
    tags=$(oras repo tags "$repo" 2>/dev/null | tail -20)

    if [[ -z "$tags" ]]; then
        echo "Error: No tags found for $repo"
        exit 1
    fi

    # Check if recommended tag exists and show it first
    if [[ -n "$default_tag" ]]; then
        if oras resolve "$repo:$default_tag" &>/dev/null; then
            echo ""
            echo "Recommended tag: $default_tag"
        fi
    fi

    echo ""
    echo "Recent tags (last 20):"
    while IFS= read -r tag; do
        printf "  %s\n" "$tag"
    done <<< "$tags"

    exit 0
}

# Validate or require a tag. If missing, print error with recommendation and exit.
require_tag() {
    local component=$1
    local provided_tag=$2
    local default_tag=$3

    if [[ -n "$provided_tag" ]]; then
        return 0
    fi

    echo "Error: --tag is required for non-interactive use."
    echo ""
    echo "Common tag for $component: $default_tag"
    echo "Run with --list-tags to see all available tags."
    exit 1
}

# Fetch created time and commit SHA for a tag
show_image_info() {
    local component=$1
    local tag=$2
    local repo="${3:-$REGISTRY/$component}"

    echo ""
    echo "=== Image info for $component:$tag ==="

    local config
    if ! config=$(oras manifest fetch-config "$repo:$tag" --platform "$PLATFORM" 2>/dev/null); then
        echo "Error: Failed to fetch image config for $repo:$tag on $PLATFORM"
        return 1
    fi

    local created
    created=$(echo "$config" | jq -r '.created // "unknown"')
    echo "Created: $created"

    local git_sha
    git_sha=$(echo "$config" | jq -r '.config.Labels["net.pingcap.tibuild.git-sha"] // "unknown"')
    echo "Git SHA: $git_sha"
}

# Show artifact info
show_artifact_info() {
    local component=$1
    local tag=$2
    local repo="${3:-$(artifact_repo_for "$component")}"

    echo ""
    echo "=== Artifact info for $component:$tag ==="
    echo "Repo: $repo"

    local manifest
    if manifest=$(oras manifest fetch "$repo:$tag" 2>/dev/null); then
        local artifact_type
        artifact_type=$(echo "$manifest" | jq -r '.artifactType // .config.mediaType // "unknown"')
        echo "Type: $artifact_type"
    else
        echo "Warning: Failed to fetch artifact manifest (this does not prevent download)"
    fi
}

# Display binaries found in extract_dir
show_found_binaries() {
    local extract_dir=$1
    local binaries
    binaries=$(find "$extract_dir" -maxdepth 1 -type f 2>/dev/null | sort)
    if [[ -z "$binaries" ]]; then
        binaries=$(find "$extract_dir" -type f 2>/dev/null | sort)
    fi
    echo "Found binaries:"
    echo "$binaries" | while read -r bin; do
        [[ -z "$bin" ]] && continue
        local name
        name=$(basename "$bin")
        local size
        size=$(stat -c%s "$bin" 2>/dev/null || stat -f%z "$bin" 2>/dev/null)
        printf "  %s (%s bytes)\n" "$name" "$size"
    done
}

# Find expected binaries recursively and copy them to extract_dir root
flatten_binaries() {
    local extract_dir=$1
    local component=$2

    case "$component" in
        tidb)
            if [[ ! -f "$extract_dir/tidb-server" ]]; then
                local found
                found=$(find "$extract_dir" -name "tidb-server" -type f 2>/dev/null | head -1)
                if [[ -n "$found" ]]; then
                    cp "$found" "$extract_dir/tidb-server"
                fi
            fi
            ;;
        pd)
            if [[ ! -f "$extract_dir/pd-server" ]]; then
                local found
                found=$(find "$extract_dir" -name "pd-server" -type f 2>/dev/null | head -1)
                if [[ -n "$found" ]]; then
                    cp "$found" "$extract_dir/pd-server"
                fi
            fi
            ;;
        tikv)
            if [[ ! -f "$extract_dir/tikv-server" ]]; then
                local found
                found=$(find "$extract_dir" -name "tikv-server" -type f 2>/dev/null | head -1)
                if [[ -n "$found" ]]; then
                    cp "$found" "$extract_dir/tikv-server"
                fi
            fi
            if [[ ! -f "$extract_dir/tikv-worker" ]]; then
                local found
                found=$(find "$extract_dir" -name "tikv-worker" -type f 2>/dev/null | head -1)
                if [[ -n "$found" ]]; then
                    cp "$found" "$extract_dir/tikv-worker"
                fi
            fi
            ;;
    esac
}

# Package binaries from extract_dir into output_dir tarballs
package_component() {
    local component=$1
    local extract_dir=$2
    local output_dir=$3

    local packaged=()

    case "$component" in
        tidb)
            if [[ -f "$extract_dir/tidb-server" ]]; then
                tar -czf "$output_dir/tidb.tar.gz" -C "$extract_dir" tidb-server
                packaged+=("tidb.tar.gz")
            fi
            ;;
        pd)
            if [[ -f "$extract_dir/pd-server" ]]; then
                tar -czf "$output_dir/pd.tar.gz" -C "$extract_dir" pd-server
                packaged+=("pd.tar.gz")
            fi
            ;;
        tikv)
            local tikv_binaries=()
            [[ -f "$extract_dir/tikv-server" ]] && tikv_binaries+=("tikv-server")
            [[ -f "$extract_dir/tikv-worker" ]] && tikv_binaries+=("tikv-worker")
            if [[ ${#tikv_binaries[@]} -gt 0 ]]; then
                tar -czf "$output_dir/tikv.tar.gz" -C "$extract_dir" "${tikv_binaries[@]}"
                packaged+=("tikv.tar.gz")
            fi
            ;;
    esac

    rm -rf "$extract_dir"

    if [[ ${#packaged[@]} -eq 0 ]]; then
        echo "Error: No known binaries packaged for $component"
        return 1
    fi

    echo ""
    echo "Packaged:"
    for pkg in "${packaged[@]}"; do
        local pkg_path="$output_dir/$pkg"
        local pkg_size
        pkg_size=$(stat -c%s "$pkg_path" 2>/dev/null || stat -f%z "$pkg_path" 2>/dev/null)
        printf "  %s (%s bytes)\n" "$pkg" "$pkg_size"
    done

    return 0
}

# Download image layers and extract binaries (tries layers from last to first)
fetch_component() {
    local component=$1
    local tag=$2
    local output_dir=$3
    local repo="${4:-$REGISTRY/$component}"

    echo ""
    echo "========================================"
    echo "Fetching $component:$tag"
    echo "========================================"

    # Get manifest
    local manifest
    manifest=$(oras manifest fetch "$repo:$tag" --platform "$PLATFORM" 2>/dev/null)
    if [[ -z "$manifest" ]]; then
        echo "Error: Failed to fetch manifest for $repo:$tag"
        return 1
    fi

    # Get layer count
    local layer_count
    layer_count=$(echo "$manifest" | jq '.layers | length')
    if [[ "$layer_count" -eq 0 ]]; then
        echo "Error: No layers found in manifest"
        return 1
    fi

    local extract_dir="/tmp/tidbx-${component}-extract-$$"
    mkdir -p "$extract_dir"

    # Try layers from last to first until we find the binaries.
    local found_layer=-1
    local prev_layer_file=""
    for ((i=layer_count - 1; i>=0; i--)); do
        local layer_digest
        layer_digest=$(echo "$manifest" | jq -r ".layers[$i].digest")
        local layer_size
        layer_size=$(echo "$manifest" | jq -r ".layers[$i].size")

        [[ -n "$prev_layer_file" ]] && rm -f "$prev_layer_file"

        echo "Trying layer $i ($layer_size bytes)..."
        local layer_file="/tmp/tidbx-${component}-${tag}-layer-${i}.tar.gz"

        if oras blob fetch "$repo@$layer_digest" --output "$layer_file" 2>/dev/null; then
            rm -rf "$extract_dir"/*
            if tar -xzf "$layer_file" -C "$extract_dir" 2>/dev/null; then
                case "$component" in
                    tidb)
                        [[ -f "$extract_dir/tidb-server" ]] && found_layer=$i
                        ;;
                    pd)
                        [[ -f "$extract_dir/pd-server" ]] && found_layer=$i
                        ;;
                    tikv)
                        [[ -f "$extract_dir/tikv-server" && -f "$extract_dir/tikv-worker" ]] && found_layer=$i
                        ;;
                esac
            fi
            rm -f "$layer_file"
            if [[ $found_layer -ge 0 ]]; then
                echo "Found binaries in layer $i"
                break
            fi
        else
            rm -f "$layer_file"
        fi
        prev_layer_file="$layer_file"
    done

    if [[ $found_layer -lt 0 ]]; then
        echo "Error: No layer containing binaries found for $component"
        rm -rf "$extract_dir"
        return 1
    fi

    show_found_binaries "$extract_dir"
    package_component "$component" "$extract_dir" "$output_dir"
}

# Download OCI artifact directly and extract binaries
fetch_component_artifact() {
    local component=$1
    local tag=$2
    local output_dir=$3
    local repo="${4:-$(artifact_repo_for "$component")}"

    echo ""
    echo "========================================"
    echo "Fetching artifact $component:$tag"
    echo "========================================"

    local temp_dir="/tmp/tidbx-${component}-artifact-$$"
    mkdir -p "$temp_dir"

    echo "Pulling artifact $repo:$tag ..."
    if ! oras pull "$repo:$tag" --output "$temp_dir"; then
        echo "Error: Failed to pull artifact $repo:$tag"
        rm -rf "$temp_dir"
        return 1
    fi

    local extract_dir="/tmp/tidbx-${component}-extract-$$"
    mkdir -p "$extract_dir"

    # Look for tarballs and extract them
    local found_tarball=false
    while IFS= read -r tarball; do
        [[ -z "$tarball" ]] && continue
        echo "Extracting $(basename "$tarball") ..."
        if tar -xzf "$tarball" -C "$extract_dir" 2>/dev/null; then
            found_tarball=true
        elif tar -xf "$tarball" -C "$extract_dir" 2>/dev/null; then
            found_tarball=true
        fi
    done < <(find "$temp_dir" \( -name "*.tar.gz" -o -name "*.tgz" -o -name "*.tar" \))

    # If no tarball found, assume flat binaries
    if [[ "$found_tarball" == false ]]; then
        find "$temp_dir" -type f -executable -exec cp {} "$extract_dir/" \; 2>/dev/null || true
    fi

    rm -rf "$temp_dir"

    # Flatten binaries from subdirectories to root
    flatten_binaries "$extract_dir" "$component"

    # Validate binaries exist
    local has_binary=false
    case "$component" in
        tidb)
            [[ -f "$extract_dir/tidb-server" ]] && has_binary=true
            ;;
        pd)
            [[ -f "$extract_dir/pd-server" ]] && has_binary=true
            ;;
        tikv)
            [[ -f "$extract_dir/tikv-server" && -f "$extract_dir/tikv-worker" ]] && has_binary=true
            ;;
    esac

    if [[ "$has_binary" == false ]]; then
        echo "Error: No binaries found in artifact for $component"
        rm -rf "$extract_dir"
        return 1
    fi

    show_found_binaries "$extract_dir"
    package_component "$component" "$extract_dir" "$output_dir"
}

# Main
COMPONENT=""
TAG=""
IMAGE_REF=""
OUTPUT_DIR="/tmp"
FETCH_ALL=false
LIST_TAGS=false

while [[ "$#" -gt 0 ]]; do
    case $1 in
        -c|--component)
            if [[ $# -lt 2 || "$2" =~ ^- ]]; then echo "Error: --component requires a value"; usage; fi
            COMPONENT="$2"
            shift 2
            ;;
        -t|--tag)
            if [[ $# -lt 2 || "$2" =~ ^- ]]; then echo "Error: --tag requires a value"; usage; fi
            TAG="$2"
            shift 2
            ;;
        -i|--image)
            if [[ $# -lt 2 || "$2" =~ ^- ]]; then echo "Error: --image requires a value"; usage; fi
            IMAGE_REF="$2"
            shift 2
            ;;
        --list-tags)
            LIST_TAGS=true
            shift
            ;;
        -o|--output)
            if [[ $# -lt 2 || "$2" =~ ^- ]]; then echo "Error: --output requires a value"; usage; fi
            OUTPUT_DIR="$2"
            shift 2
            ;;
        -a|--all)
            FETCH_ALL=true
            shift
            ;;
        -h|--help)
            usage
            ;;
        *)
            echo "Unknown option: $1"
            usage
            ;;
    esac
done

# Validate component enum early
if [[ -n "$COMPONENT" && "$COMPONENT" != "tidb" && "$COMPONENT" != "tikv" && "$COMPONENT" != "pd" ]]; then
    echo "Error: Invalid component '$COMPONENT'. Must be one of: tidb, tikv, pd"
    exit 1
fi

if [[ -n "$IMAGE_REF" ]]; then
    if [[ "$FETCH_ALL" == true ]]; then
        echo "Error: --image can only be used with one --component, not --all"
        exit 1
    fi
    if [[ -z "$COMPONENT" ]]; then
        echo "Error: --image requires --component <tidb|tikv|pd> so output tarball names are known"
        exit 1
    fi
    if [[ -n "$TAG" ]]; then
        echo "Error: --image and --tag are mutually exclusive"
        exit 1
    fi
fi

if [[ "$LIST_TAGS" == true && "$FETCH_ALL" == true ]]; then
    echo "Error: --list-tags cannot be combined with --all. Use --component <tidb|tikv|pd> --list-tags."
    exit 1
fi

check_prerequisites

CUSTOM_REPO=""
CUSTOM_TAG=""
if [[ -n "$IMAGE_REF" ]]; then
    parse_image_ref "$IMAGE_REF" CUSTOM_REPO CUSTOM_TAG || exit 1
fi

mkdir -p "$OUTPUT_DIR"

if [[ "$FETCH_ALL" == true ]]; then
    COMPONENTS=("tidb" "tikv" "pd")
else
    if [[ -z "$COMPONENT" ]]; then
        echo "Error: --component is required (or use --all)"
        usage
    fi
    COMPONENTS=("$COMPONENT")
fi

failed_components=()

for comp in "${COMPONENTS[@]}"; do
    SELECTED_TAG="${CUSTOM_TAG:-$TAG}"

    if [[ -n "$CUSTOM_REPO" ]]; then
        SELECTED_REPO="$CUSTOM_REPO"
    else
        SELECTED_REPO=""
    fi

    MODE=$(detect_mode "$SELECTED_REPO" "$SELECTED_TAG")

    if [[ -z "$SELECTED_REPO" ]]; then
        if [[ "$MODE" == "artifact" ]]; then
            SELECTED_REPO=$(artifact_repo_for "$comp")
        else
            SELECTED_REPO="$REGISTRY/$comp"
        fi
    fi

    DEFAULT_TAG=$(default_tag_for "$comp" "$MODE")

    if [[ -z "$SELECTED_TAG" && "$FETCH_ALL" == true ]]; then
        SELECTED_TAG="$DEFAULT_TAG"
    fi

    # Local validation before network
    if [[ "$LIST_TAGS" == false ]]; then
        require_tag "$comp" "$SELECTED_TAG" "$DEFAULT_TAG"
    fi

    # Auth before list-tags or fetch
    check_gcr_auth "$SELECTED_REPO" "${SELECTED_TAG:-$DEFAULT_TAG}"

    if [[ "$LIST_TAGS" == true ]]; then
        list_tags "$comp" "$SELECTED_REPO" "$DEFAULT_TAG"
    fi

    if [[ "$MODE" == "artifact" ]]; then
        if ! show_artifact_info "$comp" "$SELECTED_TAG" "$SELECTED_REPO"; then
            echo "Warning: Failed to show artifact info for $comp, continuing download..."
        fi
        if ! fetch_component_artifact "$comp" "$SELECTED_TAG" "$OUTPUT_DIR" "$SELECTED_REPO"; then
            failed_components+=("$comp")
            continue
        fi
    else
        if ! show_image_info "$comp" "$SELECTED_TAG" "$SELECTED_REPO"; then
            echo "Warning: Failed to show image info for $comp, continuing download..."
        fi
        if ! fetch_component "$comp" "$SELECTED_TAG" "$OUTPUT_DIR" "$SELECTED_REPO"; then
            failed_components+=("$comp")
            continue
        fi
    fi
done

if [[ ${#failed_components[@]} -gt 0 ]]; then
    echo ""
    echo "========================================"
    echo "Failed components: ${failed_components[*]}"
    echo "========================================"
    exit 1
fi

echo ""
echo "========================================"
echo "All done. Output directory: $OUTPUT_DIR"
echo "========================================"
ls -lh "$OUTPUT_DIR"/*.tar.gz 2>/dev/null || true

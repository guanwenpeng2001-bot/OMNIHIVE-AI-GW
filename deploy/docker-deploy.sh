#!/bin/bash
# =============================================================================
# OmniHive Docker Deployment Preparation Script
# =============================================================================
# This script prepares deployment files for OmniHive:
#   - Downloads docker-compose.local.yml and .env.example
#   - Generates secure secrets (JWT_SECRET, TOTP_ENCRYPTION_KEY, POSTGRES_PASSWORD)
#   - Creates necessary data directories
#
# After running this script, you can start services with:
#   docker-compose up -d
# =============================================================================

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Pin this preparation script to its release; update this tag with the script.
# Operators may explicitly select another release with OMNIHIVE_RELEASE_TAG.
DEFAULT_RELEASE_TAG="v0.3.8"
RELEASE_TAG="${OMNIHIVE_RELEASE_TAG:-$DEFAULT_RELEASE_TAG}"
GITHUB_RAW_URL="https://raw.githubusercontent.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/${RELEASE_TAG}/deploy"

# Print colored message
print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Generate random secret
generate_secret() {
    openssl rand -hex 32
}

# Check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Main installation function
main() {
    local staging_dir=""
    trap 'if [ -n "$staging_dir" ]; then rm -rf -- "$staging_dir"; fi' EXIT
    echo ""
    echo "=========================================="
    echo "  OmniHive Deployment Preparation"
    echo "=========================================="
    echo ""

    # Check if openssl is available
    if ! command_exists openssl; then
        print_error "openssl is not installed. Please install openssl first."
        exit 1
    fi

    # Protect each local file independently; never replace an existing deployment.
    if [ -e "docker-compose.yml" ] || [ -e ".env" ]; then
        print_error "docker-compose.yml or .env already exists; refusing to overwrite either file."
        exit 1
    fi

    staging_dir=$(mktemp -d "./.omnihive-deploy.XXXXXX")

    # Download docker-compose.local.yml and save as docker-compose.yml
    print_info "Downloading docker-compose.yml..."
    if command_exists curl; then
        curl -fsSL --globoff "${GITHUB_RAW_URL}/docker-compose.local.yml" -o "${staging_dir}/docker-compose.yml"
    elif command_exists wget; then
        wget -q "${GITHUB_RAW_URL}/docker-compose.local.yml" -O "${staging_dir}/docker-compose.yml"
    else
        print_error "Neither curl nor wget is installed. Please install one of them."
        exit 1
    fi
    print_success "Downloaded docker-compose.yml"

    # Download .env.example
    print_info "Downloading .env.example..."
    if command_exists curl; then
        curl -fsSL --globoff "${GITHUB_RAW_URL}/.env.example" -o "${staging_dir}/.env.example"
    else
        wget -q "${GITHUB_RAW_URL}/.env.example" -O "${staging_dir}/.env.example"
    fi
    print_success "Downloaded .env.example"

    if ! grep -q '^POSTGRES_PASSWORD=' "${staging_dir}/.env.example" ||
        ! grep -q '^JWT_SECRET=' "${staging_dir}/.env.example" ||
        ! grep -q '^TOTP_ENCRYPTION_KEY=' "${staging_dir}/.env.example"; then
        print_error "Downloaded .env.example is missing required configuration keys."
        exit 1
    fi

    # Same-filesystem renames publish only complete downloads. Recheck the
    # destinations in case another process created them during the download.
    if [ -e "docker-compose.yml" ] || [ -e ".env" ]; then
        print_error "A deployment file appeared during download; refusing to overwrite it."
        exit 1
    fi
    mv "${staging_dir}/docker-compose.yml" docker-compose.yml
    mv "${staging_dir}/.env.example" .env.example
    rm -rf -- "$staging_dir"
    staging_dir=""

    # Generate .env file with auto-generated secrets
    print_info "Generating secure secrets..."
    echo ""

    (
        set +x
        umask 077
        tmp_env=$(mktemp .env.XXXXXXXX)
        trap 'rm -f "$tmp_env"' EXIT
        while IFS= read -r line || [[ -n $line ]]; do
            case "$line" in
                JWT_SECRET=*|TOTP_ENCRYPTION_KEY=*|POSTGRES_PASSWORD=*) continue ;;
            esac
            printf '%s\n' "$line"
        done < .env.example > "$tmp_env"
        for name in JWT_SECRET TOTP_ENCRYPTION_KEY POSTGRES_PASSWORD; do
            value=$(generate_secret)
            printf '%s=%s\n' "$name" "$value" >> "$tmp_env"
        done
        mv -f "$tmp_env" .env
    )

    # Create data directories
    print_info "Creating data directories..."
    mkdir -p data postgres_data redis_data
    print_success "Created data directories"

    # Set secure permissions for .env file (readable/writable only by owner)
    chmod 600 .env
    echo ""

    # Display completion message
    echo "=========================================="
    echo "  Preparation Complete!"
    echo "=========================================="
    echo ""
    print_info "Credentials file: $(pwd)/.env"
    echo ""
    echo "Directory structure:"
    echo "  docker-compose.yml        - Docker Compose configuration"
    echo "  .env                      - Environment variables (generated secrets)"
    echo "  .env.example              - Example template (for reference)"
    echo "  data/                     - Application data (will be created on first run)"
    echo "  postgres_data/            - PostgreSQL data"
    echo "  redis_data/               - Redis data"
    echo ""
    echo "Next steps:"
    echo "  1. (Optional) Edit .env to customize configuration"
    echo "  2. Start services:"
    echo "     docker-compose up -d"
    echo ""
    echo "  3. View logs:"
    echo "     docker-compose logs -f omnihive"
    echo ""
    echo "  4. Access Web UI:"
    echo "     http://localhost:8080"
    echo "     For remote access, configure Caddy TLS; do not expose this HTTP port publicly."
    echo ""
    print_info "If admin password is not set in .env, it will be auto-generated."
    print_info "First-start logs identify the private admin password file."
    echo ""
}

# Run main function
main "$@"

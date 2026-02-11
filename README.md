# fetch-jwks

A Go command-line tool for fetching and caching JWKS documents from OAuth2/OIDC issuers. It supports retries with backoff and conditional requests via ETag/If-None-Match to avoid unnecessary downloads.

## Getting Started

- Build: `make build`
- Test: `make test`
- Format: `make fmt`
- Lint: `make lint`

Requires Go 1.22+.

## Pre-commit

Install hooks once per clone: `pre-commit install`. Run them manually with `pre-commit run --all-files`. Hooks include gofmt, golangci-lint (--fast), and basic whitespace checks.

## Devcontainer / Codespaces

A devcontainer is included for GitHub Codespaces or VS Code Remote - Containers. It installs Go 1.22, golangci-lint v2.1.0, and pre-commit, then runs `pre-commit install --install-hooks` on create.

## Configuration

See [examples/fetch-jwks.example.yaml](examples/fetch-jwks.example.yaml) for a minimal config. You can also specify issuers via repeatable `-issuer issuer=<url>,jwks_uri=<url>` flags.
## RPM Packaging

This project includes RPM packaging that follows the [Fedora Go Packaging Guidelines](https://docs.fedoraproject.org/en-US/packaging-guidelines/Golang/).

### Building RPMs Locally

To build RPMs:

```bash
# Install dependencies (Fedora/EPEL)
sudo dnf install go-rpm-macros go-vendor-tools golang rpm-build rpmdevtools

# Set up rpmbuild tree
rpmdev-setuptree

# Create source tarball
version="0.0.0"  # replace with actual version
tar czf ~/rpmbuild/SOURCES/fetch-jwks-${version}.tar.gz \
  --transform "s,^\./,fetch-jwks-${version}/," \
  --exclude=.git .

# Generate vendor tarball
cd ~/rpmbuild/SOURCES
tar xzf fetch-jwks-${version}.tar.gz
cd fetch-jwks-${version}
go-vendor-archive -f fetch-jwks -v ${version}
mv fetch-jwks-${version}-vendor.tar.gz ~/rpmbuild/SOURCES/

# Build RPM
cd /path/to/fetch-jwks
rpmbuild -ba rpm/fetch-jwks.spec --define "version ${version}"
```

### CI/CD

- **RPM Build Test**: Automatically tests RPM builds on Fedora 39, Fedora 40, and EPEL 9 for PRs and pushes
- **Release**: Creates RPMs and GitHub releases when tags are pushed
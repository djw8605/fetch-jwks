# RPM Packaging for fetch-jwks

This directory contains RPM packaging files for fetch-jwks that comply with [Fedora Go Packaging Guidelines](https://docs.fedoraproject.org/en-US/packaging-guidelines/Golang/).

## Files

- `fetch-jwks.spec` - RPM spec file following Fedora Go packaging standards

## Compliance with Fedora Go Guidelines

The spec file follows these requirements:

✅ **Vendored Dependencies**: Uses go-vendor-tools to vendor all Go module dependencies  
✅ **License Handling**: Includes cumulative SPDX license expression and all vendor licenses  
✅ **Bundled Provides**: Automatically generates `bundled(golang(...))` provides via vendor/modules.txt  
✅ **go-rpm-macros**: Uses standard macros like `%gobuild`, `%gocheck`, `%goprep`  
✅ **Naming**: No `golang-` prefix (user-facing application)  

## Supported Platforms

The RPM is designed to build on:
- Fedora 39+
- EPEL 9 (RHEL 9, Rocky Linux 9, AlmaLinux 9)

## Building

See the main [README.md](../README.md#rpm-packaging) for build instructions.

## Testing

RPM builds are automatically tested in CI on multiple platforms:
- GitHub Actions workflow: `.github/workflows/rpm-test.yml`
- Tests on: Fedora 39, Fedora 40, Rocky Linux 9, AlmaLinux 9

## Version Management

- Development builds use `0.0.0` version with test release numbers
- Release builds extract version from git tags (e.g., `v1.2.3` → version `1.2.3`)

%global goipath         fetch-jwks
%global forgeurl        https://github.com/djw8605/fetch-jwks

Version:        0.0.0

%gometa -f

Name:           fetch-jwks
Release:        1%{?dist}
Summary:        CLI tool for fetching and caching JWKS documents from OAuth2/OIDC issuers

# Main package is Apache-2.0
# Vendored dependencies include:
# - gopkg.in/yaml.v3: MIT OR Apache-2.0
License:        Apache-2.0 AND (MIT OR Apache-2.0)
URL:            %{gourl}
Source0:        %{forgeurl}/archive/v%{version}/fetch-jwks-%{version}.tar.gz
# Generate vendor tarball with:
# go_vendor_archive create fetch-jwks.spec
Source1:        %{goipath}-%{version}-vendor.tar.gz

BuildRequires:  go-rpm-macros
BuildRequires:  go-vendor-tools
BuildRequires:  golang

%description
fetch-jwks is a Go command-line tool for fetching and caching JWKS
(JSON Web Key Set) documents from OAuth2/OIDC issuers. It supports
retries with exponential backoff and conditional requests via
ETag/If-None-Match to avoid unnecessary downloads.

%prep
%goprep -k
%go_vendor_archive_extract -a 1

%generate_buildrequires
%go_generate_buildrequires

%build
%gobuild -o %{gobuilddir}/bin/fetch-jwks %{goipath}/cmd/fetch-jwks

%install
install -D -m 0755 %{gobuilddir}/bin/fetch-jwks %{buildroot}%{_bindir}/fetch-jwks
install -D -m 0644 examples/fetch-jwks.example.yaml %{buildroot}%{_sysconfdir}/fetch-jwks.conf
install -d -m 0755 %{buildroot}%{_sysconfdir}/fetch-jwks.config.d
install -d -m 0755 %{buildroot}%{_localstatedir}/cache/jwks

%check
%gocheck

%files
%license LICENSE
%license _licenses/*
%license vendor/modules.txt
%doc README.md examples/fetch-jwks.example.yaml
%{_bindir}/fetch-jwks
%config(noreplace) %{_sysconfdir}/fetch-jwks.conf
%dir %{_sysconfdir}/fetch-jwks.config.d
%dir %{_localstatedir}/cache/jwks

%changelog
* Wed Feb 11 2026 Derek Weitzel <dweitzel@unl.edu> - 0.0.0-1
- Initial RPM packaging

%global goipath         fetch-jwks
%global forgeurl        https://github.com/djw8605/fetch-jwks

Version:        0.0.0

%gometa -f

Name:           fetch-jwks
Release:        1%{?dist}
Summary:        CLI tool for fetching and caching JWKS documents from OAuth2/OIDC issuers

License:        Apache-2.0
URL:            %{gourl}
Source0:        %{forgeurl}/archive/v%{version}/fetch-jwks-%{version}.tar.gz

BuildRequires:  go-rpm-macros
BuildRequires:  golang

%description
fetch-jwks is a Go command-line tool for fetching and caching JWKS
(JSON Web Key Set) documents from OAuth2/OIDC issuers. It supports
retries with exponential backoff and conditional requests via
ETag/If-None-Match to avoid unnecessary downloads.

%prep
%goprep -k
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
%doc README.md examples/fetch-jwks.example.yaml
%{_bindir}/fetch-jwks
%config(noreplace) %{_sysconfdir}/fetch-jwks.conf
%dir %{_sysconfdir}/fetch-jwks.config.d
%dir %{_localstatedir}/cache/jwks

%changelog

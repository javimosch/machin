# Vendored CA root bundle

`cacert.pem.gz` — a snapshot of the Mozilla root certificate store as shipped by
Debian/Ubuntu's `ca-certificates` package (**version 20260601~22.04.1**, 166 root
CAs), extracted from `/etc/ssl/certs/ca-certificates.crt` and gzipped. This is the
same root store curl vendors at <https://curl.se/ca/cacert.pem> — a plain PEM bundle
of public trust anchors, freely redistributable (they're public certificates, not
secrets).

Embedded into the `machin` binary (`//go:embed`, gzipped ≈ 142 KB) and compiled
directly into a program's C source (as a byte array, see `mfl_ca_bundle_pem` in
`build.go`) by `machin build --static` when the program uses TLS — so a static
binary can verify server certificates with **zero external files**, not even a
system CA store. See issue #283.

**This is a build-time snapshot, not a live trust store.** A dynamic (non-static)
build ignores this entirely and trusts the live system CA store instead (correct —
it gets security updates from the OS). Static builds add this bundle *alongside*
whatever system store is present at link/runtime, as a fallback for environments
(e.g. `FROM scratch`) that have none. Rebuild and redeploy periodically to pick up
CA store changes (a compromised/revoked root doesn't un-revoke itself in an old
binary).

To update: copy `/etc/ssl/certs/ca-certificates.crt` from a current Debian/Ubuntu
system (or fetch <https://curl.se/ca/cacert.pem>), gzip it
(`gzip -9 -c ca-certificates.crt > cacert.pem.gz`), and bump the version here.

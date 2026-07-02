# Benchmark — a TLS-calling binary, FROM scratch

`bench/cold-start` measures the libc+SQLite case: a 92.9 kB binary, `FROM scratch`.
That number does **not** apply to the more common shape — an app that actually
calls out over HTTPS (which is most real backends: webhooks, an LLM API, a
payment provider, ...). This benchmark measures that case honestly, as its own
number, not folded into the smaller one. See issue #283.

## Results (this machine)

| | size | ships |
|---|--:|---|
| dynamic (`machin build`) | 26.5 kB | needs `libssl.so`/`libcrypto.so` on the host |
| `--static` (stripped) | **5.28 MB** | `FROM scratch` — genuinely zero external files |

Verified two ways:
- **Positive**: the stripped static binary, copied into an empty Docker `FROM
  scratch` image (no `/etc`, no shared libs, no CA bundle file), makes a real
  HTTPS request with full certificate verification against a real public host.
- **Negative**: the same binary, pointed at a known self-signed/untrusted
  certificate, **rejects it** — proving verification is genuinely active, not
  silently disabled for the sake of "it works."

## Why the jump from 92.9 kB to 5.28 MB

OpenSSL, statically linked, plus an embedded ~245 KB CA root bundle (so the binary
can verify certificates with zero external trust store — see
`vendor/certs/README.md`). That's a large static archive; there's no getting
around it without dropping OpenSSL for a different TLS stack (a much bigger,
separate project — see the "sizing honesty" note in issue #283).

**Still a big win relative to the usual alternative**: `bench/cold-start` measured
Node at 178 MB for a *plaintext* hello server — a real HTTPS-calling Node/Python
service, with its base image and dependencies, lands well north of that. 5.28 MB,
one file, zero dependencies, is a different category.

## Reproduce

```bash
./run.sh   # builds dynamic + static, measures both, runs the static binary for
           # real, and (if docker is installed) verifies the FROM-scratch case
```

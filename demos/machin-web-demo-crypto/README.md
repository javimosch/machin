# machin-web-demo-crypto

Cryptography REST API playground built with [machweb](../../framework/machweb.src).

**Exercises:** `sha256`, `hmac_sha256`, `rand_bytes`, `aes_gcm_encrypt/decrypt`, `ed25519_pub/sign/verify`, `x25519_pub/x25519_shared`, hex encode/decode, combined request struct (MFL's `parse` is monomorphic — one struct covers all endpoints).

## Build & run

```sh
./build.sh
./app          # → http://localhost:48080
```

Open the browser for an interactive playground, or use the API directly.

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | HTML playground |
| POST | `/hash` | Body: raw text → `{"hash":"<sha256hex>"}` |
| POST | `/hmac` | `{key, msg}` → `{"hmac":"<hex>"}` |
| POST | `/encrypt` | `{key_hex, plaintext}` (empty key_hex generates random key) → `{key_hex, iv_hex, ct_hex}` |
| POST | `/decrypt` | `{key_hex, iv_hex, ct_hex}` → `{"plaintext":"..."}` |
| GET | `/keygen/ed25519` | `{"priv_hex","pub_hex"}` |
| POST | `/sign` | `{priv_hex, msg}` → `{"sig_hex":"..."}` |
| POST | `/verify` | `{pub_hex, msg, sig_hex}` → `{"valid":true/false}` |
| GET | `/keygen/x25519` | `{"priv_hex","pub_hex"}` |
| POST | `/dh` | `{my_priv_hex, their_pub_hex}` → `{"shared_hex":"..."}` |

## Gap noted

`parse(body, T{})` is monomorphic in MFL — a handler that parses multiple struct shapes must use a single combined struct or `json_get`. Demo uses a combined `CryptoReq` struct.

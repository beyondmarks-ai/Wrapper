# Security and privacy

## Security boundary

Wrapper Cloud is a coordination and temporary ciphertext service. It is not trusted with file contents, file names, requested paths, search results, or transfer manifests.

Each Windows installation creates:

- an age X25519 encryption identity;
- an Ed25519 request/event signing identity;
- Firebase refresh credentials after Google login.

Private material is encrypted at rest with Windows DPAPI for the current user. The local HTTP API is carried over a named pipe whose ACL permits only that user SID and SYSTEM.

## Protocol protections

- Every cloud API request requires a valid Firebase ID token.
- Device operations additionally sign method, escaped path, query, timestamp, nonce, and SHA-256 body digest.
- Nonces are transactionally recorded and expire through Firestore TTL; stale timestamps and replays are rejected.
- Pairing codes are random, stored only as hashes, expire after ten minutes, and require source-PC confirmation.
- Events are encrypted to the destination device and signed by the source device.
- Archives are compressed and age-encrypted locally before upload.
- Decryption stages files in a temporary directory, rejects traversal, symlinks, and special files, verifies the encrypted manifest and every SHA-256, then moves verified output into place.
- Name conflicts default to keep-both instead of silent overwrite.
- Remote search and remote-initiated transfer are restricted to explicit shared roots.
- A transfer is capped at 20 GiB; each beta user is capped at 100 transfers and 50 GiB of ciphertext per rolling 24 hours.

## Cloud-visible metadata

The service can see account and device identifiers, device display names and public keys, source and target device IDs, timestamps, transfer state, ciphertext size, IP/network metadata, and encrypted payload lengths. It cannot decrypt event bodies, paths, manifests, archives, or files.

A dedicated desktop Firebase API key is restricted to Identity Toolkit and Secure Token endpoints. Like all installed desktop application configuration, it is publicly recoverable from the binary and is not treated as a secret; Firebase ID tokens and device signatures provide authentication.

Google login uses Authorization Code with PKCE and an exact 127.0.0.1 loopback callback. The desktop sends the one-time code and verifier only to the HTTPS Wrapper Cloud exchange endpoint. The OAuth client secret is injected into Cloud Run from Secret Manager, is never present in release binaries or GitHub variables, and upstream OAuth error details are not returned to clients.

Google Cloud Storage receives ciphertext only. Signed upload/download URLs are short-lived and always HTTPS. Objects are scheduled for deletion after 24 hours, have a one-day lifecycle safety net, and use a zero-duration soft-delete policy so deleted ciphertext is not retained for the Cloud Storage default recovery window. Firestore TTL removes transient event, nonce, pairing-code, and transfer documents.

## Threats not solved

- Malware or an attacker controlling an unlocked paired PC can read files that user account can read and can access already downloaded files.
- Revocation prevents future authenticated activity; it cannot erase data already downloaded by another device.
- End-to-end encryption prevents server-side malware scanning. Recipients must treat transferred files as untrusted and use endpoint protection.
- A malicious paired device can request paths only under shared roots, but filenames and contents inside those roots are intentionally available to that paired device.
- Traffic analysis and ciphertext sizes remain visible to the cloud provider.
- Availability depends on Firebase, Cloud Run, Firestore, Cloud Tasks, GCS, and the source PC being online for live search or requested upload.
- DPAPI protection inherits the security of the Windows account. It does not protect against code already running as that user.

## Reporting and response

Do not include tokens, pairing codes, signed URLs, private keys, file names, or personal paths in bug reports. Revoke affected devices, stop the agent, sign out, and rotate the Firebase/Google configuration if credentials are suspected compromised.

Production logs must exclude authorization headers, ciphertext, signed URLs, search terms, and local paths. Retain only structured error codes, request IDs, device IDs, state transitions, latency, and byte counts for the minimum operational period.

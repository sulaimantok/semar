# Extra CA certificates

PEM files placed here (or in the directory named by `SEMAR_EXTRA_CA_DIR`) are trusted by semar-api and semar-worker in addition to the system roots, for provider endpoints signed by a private CA (for example a cert-manager `selfsigned-ca`). Use this instead of a TLS fingerprint when a cloud's endpoints each have their own certificate from one CA.

Certificates are public; still, keep this directory for CA certificates only, never private keys.

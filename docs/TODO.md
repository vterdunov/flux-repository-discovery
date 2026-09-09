# Deferred features

The v1 HTTP API runs without authentication. The following features are deferred beyond v1. Current behavior is documented in the [README](../README.md).

## CLI login through an IdP, initially Google Workspace

Proposed user workflow:

```sh
flux-repository-discovery login --server https://discovery.example.com
flux-repository-discovery dry-run --config candidate.yaml --against https://discovery.example.com
flux-repository-discovery logout --server https://discovery.example.com
```

- [ ] Register an OAuth client of the appropriate type for the CLI and define server settings for issuer, client ID / accepted audience, and access rules. Client configuration is not a secret that establishes user trust.
- [ ] Have `login` open the system browser for IdP sign-in. Use Authorization Code with PKCE S256, state, nonce, and a loopback callback on a random port. Google desktop clients support this callback. [Google installed-app flow](https://developers.google.com/identity/protocols/oauth2/native-app).
- [ ] Obtain an ID token for the agreed client ID. Do not treat an arbitrary Google API access token as proof of access to this service. Send the token in `Authorization: Bearer` only over HTTPS and only to the destination configured in the server profile.
- [ ] Verify tokens in service middleware: JWKS signature, allowed algorithm, issuer, audience, and expiry. Cache keys with rotation support. Use a maintained OIDC library instead of implementing a general-purpose JWT verifier. [Google ID token verification](https://developers.google.com/identity/gsi/web/guides/verify-google-id-token).
- [ ] Separate authentication from authorization. The IdP establishes identity; the service grants operations. Use the verified `hd` claim to restrict Google Workspace access and the issuer + subject pair as the stable identity. An email address's domain does not prove Workspace membership. [Google ID token claims](https://developers.google.com/identity/openid-connect/reference).
- [ ] Start with explicit dry-run access for a Workspace domain and/or a user allowlist. Define rule semantics before implementation. Do not assume standard ID tokens include Google Groups; investigate group requirements and a separate Directory API integration independently.
- [ ] Separate access to inputs, dry-run, and diagnostics. Noninteractive Flux access needs its own machine authentication method; browser-based CLI login does not cover it. Consider bearer token support through `ResourceSetInputProvider.secretRef` when choosing that method. [Flux ExternalService authentication](https://fluxoperator.dev/docs/crd/resourcesetinputprovider/#type).
- [ ] Choose secure storage for CLI refresh tokens, using the operating system's credential store if appropriate. Add session refresh, logout, and isolated profiles for different servers. Keep tokens out of the main project configuration, query strings, and logs.
- [ ] Define access lifetime and revocation behavior, including Workspace account changes. Avoid indefinite authorization caching.
- [ ] Add independent TDD tests for allowed and denied domains, missing `hd`, invalid audience/issuer/signature, expiry, key rotation, state substitution, login/refresh failures, denied dry-run access, and token leakage.

Recheck the selected IdP's current requirements before implementation. If verification moves to an external authentication proxy, define its trust boundary and prevent bypass. V1 does not trust arbitrary user identity headers.

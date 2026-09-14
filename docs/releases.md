# Releases

All container images and releases use GoReleaser OSS, pinned in
[mise.toml](../mise.toml), and the existing
[GitHub Actions workflow](../.github/workflows/ci.yaml). The shared configuration is
[.goreleaser.yaml](../.goreleaser.yaml). No GoReleaser Pro license or additional
publishing token is required.

| Artifact | Platforms | Example for 0.1.0 |
| --- | --- | --- |
| GHCR container | `linux/amd64`, `linux/arm64` | `ghcr.io/vterdunov/flux-repository-discovery:0.1.0` |
| CLI archives | Linux/macOS, amd64/arm64 | `flux-repository-discovery_0.1.0_linux_amd64.tar.gz` |
| SHA256 checksums | All four archives | `checksums.txt` |
| GitHub Release | Tag, notes and downloads | `v0.1.0` |

The `version` command prints `0.1.0` in both archives and the release container.
All containers use [Dockerfile.release](../Dockerfile.release) with a distroless
nonroot base and Linux binaries built by GoReleaser. Development images are
published by `docker/build-push-action`; versioned images use GoReleaser's
[Docker v2 integration](https://goreleaser.com/customization/package/dockers_v2/).
Both use Buildx with a build context containing only the prebuilt Linux binaries.

## Version and image tags

- Git tags use `vMAJOR.MINOR.PATCH`, for example `v0.1.0`.
  Build metadata (`+...`) is not supported because container tags cannot contain `+`.
- A release image uses the version without `v`, for example `:0.1.0`.
- `:latest` follows the most recently published stable version. Publish stable
  versions in increasing order. Pin an exact version or image digest in deployments.
- Prereleases such as `v0.2.0-rc.1` publish `:0.2.0-rc.1` and are marked as
  prereleases on GitHub. They do not update `:latest`.
- `:main`, `:pr-<number>` and `:sha-<commit>` are development images from the
  tested GoReleaser snapshot. Their `version` output is `0.0.0-dev-<commit>`.
  Main pushes do not update `:latest`.

Before 1.0, use patch releases for compatible fixes and minor releases for new
features or breaking changes. Describe breaking changes explicitly in the release
notes. Keep published tags and assets unchanged; publish another version for fixes.

## Publish a version

1. Merge the intended changes and release configuration to `main` and check its CI.
2. From a clean checkout, create an annotated tag on the reviewed main commit:

   ```sh
   git fetch origin --tags
   git tag -a v0.1.0 origin/main -m 'Release v0.1.0'
   git push origin refs/tags/v0.1.0
   ```

3. Watch the CI run for that tag. It runs the normal tests/lint, live GitHub E2E,
   and release packaging/image smoke test before starting the publishing job.
4. The publishing job verifies that the tag targets main history and that this
   version has not already been published. GoReleaser builds the four archives,
   checksums, and two-platform image, then uploads a draft GitHub Release. The
   final step publishes the completed release.
5. Verify the release assets and container version:

   ```sh
   gh release view v0.1.0 --repo vterdunov/flux-repository-discovery
   docker run --rm ghcr.io/vterdunov/flux-repository-discovery:0.1.0 version
   ```

Publishing jobs use the workflow's `GITHUB_TOKEN` for registry/release access and
GitHub OIDC (`id-token: write`) for keyless Cosign signatures.
Live E2E continues to use the separate metadata-only `FRD_E2E_GITHUB_TOKEN` secret.

## Verification before tagging

After tests pass, every PR, main push, and version tag runs a snapshot packaging
job. It uses the actual GoReleaser config to build all four archives and both
container platforms without publishing. It verifies archive checksums, image
architectures, and the native Linux container's `version` output. For main pushes
and same-repository PRs except Dependabot, `docker/build-push-action` then packages
the same binaries and publishes development tags, SBOMs, and provenance. It does
not compile Go. Fork and Dependabot PRs only build and verify.
Ordinary `mise run check` also validates the GoReleaser configuration.

For local validation without a Docker daemon or publishing credentials:

```sh
mise install
mise run lint-release
mise run release-snapshot
```

This writes archives, binaries, checksums and metadata under ignored `dist/`.
Snapshot versions are `0.0.0-dev-<commit>`. The local task explicitly skips Docker
and publishing; the CI packaging job additionally builds and tests the images.

## Container signatures

New development and release images are signed by digest with Cosign using GitHub
OIDC. CI verifies the signature against the exact workflow identity and issuer;
no signing key or additional secret is required. A release is published on GitHub
only after signature verification passes. Existing image versions are not signed
retroactively.

To verify a signed release, use its tag and published multi-platform digest:

```sh
tag=vX.Y.Z
image=ghcr.io/vterdunov/flux-repository-discovery@sha256:REPLACE_WITH_DIGEST
cosign verify "$image" \
  --certificate-identity "https://github.com/vterdunov/flux-repository-discovery/.github/workflows/ci.yaml@refs/tags/$tag" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Development identities end in `@refs/heads/main` or `@refs/pull/<number>/merge`.
Use the release tag identity when verifying a release for deployment.

## Failed or interrupted releases

A failure can leave an incomplete GitHub Release draft. Fix an external issue such as
registry availability, then rerun the failed job for the same tag. The workflow
can reuse an existing draft and replace its incomplete assets. Published releases
are rejected by the workflow before publication starts.

If source or release configuration needs a fix, merge it and use a new version
tag. Do not move an existing tag. Registry pushes and GitHub Release publication
are separate operations, so an image can exist even while the corresponding
GitHub Release is still a draft. A successful workflow is the completion signal.

Publishing jobs run serially and tag runs are not canceled by newer commits.

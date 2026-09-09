# Releases

Releases use GoReleaser OSS, pinned in [mise.toml](../mise.toml), and the existing
[GitHub Actions workflow](../.github/workflows/ci.yaml). The release configuration
is [.goreleaser.yaml](../.goreleaser.yaml). No GoReleaser Pro license or additional
publishing token is required.

| Artifact | Platforms | Example for 0.1.0 |
| --- | --- | --- |
| GHCR container | `linux/amd64`, `linux/arm64` | `ghcr.io/vterdunov/flux-repository-discovery:0.1.0` |
| CLI archives | Linux/macOS, amd64/arm64 | `flux-repository-discovery_0.1.0_linux_amd64.tar.gz` |
| SHA256 checksums | All four archives | `checksums.txt` |
| GitHub Release | Tag, notes and downloads | `v0.1.0` |

The `version` command prints `0.1.0` in both archives and the release container.
The container copies the Linux binaries built by GoReleaser, using
[Dockerfile.release](../Dockerfile.release) and the same distroless nonroot base
as development images. GoReleaser's [Docker v2 integration](https://goreleaser.com/customization/package/dockers_v2/)
uses Buildx to assemble both platforms from the prebuilt binaries.

## Version and image tags

- Git tags use `vMAJOR.MINOR.PATCH`, for example `v0.1.0`.
  Build metadata (`+...`) is not supported because container tags cannot contain `+`.
- A release image uses the version without `v`, for example `:0.1.0`.
- `:latest` follows the most recently published stable version. Publish stable
  versions in increasing order. Pin an exact version or image digest in deployments.
- Prereleases such as `v0.2.0-rc.1` publish `:0.2.0-rc.1` and are marked as
  prereleases on GitHub. They do not update `:latest`.
- `:main`, `:pr-<number>` and `:sha-<commit>` remain development images.
  Main pushes no longer update `:latest`.

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

The repository is private. Downloads require repository access, and pulling its
private GHCR package requires registry authentication. The workflow uses its own
`GITHUB_TOKEN` with `contents: write` and `packages: write` only in publishing jobs.
Live E2E continues to use the separate metadata-only `FRD_E2E_GITHUB_TOKEN` secret.

## Verification before tagging

Every PR, main push, and version tag runs a snapshot packaging job. It uses the
actual GoReleaser config to build all four archives and both container platforms
without publishing. It verifies archive checksums, image architectures, and the
native Linux container's `version` output. Ordinary `mise run check` also validates
the GoReleaser configuration.

For local validation without a Docker daemon or publishing credentials:

```sh
mise install
mise run lint-release
mise run release-snapshot
```

This writes archives, binaries, checksums and metadata under ignored `dist/`.
Snapshot versions are `0.0.0-dev-<commit>`. The local task explicitly skips Docker
and publishing; the CI packaging job additionally builds and tests the images.

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

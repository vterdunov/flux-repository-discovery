# Flux compatibility contract

Verified on 2026-09-09 with Go 1.27.1 on macOS arm64:

- Flux Operator `v0.59.0`, commit `e0f71db839116724c84caf8e06ab8423e7d9b174`.
- Kubernetes envtest `1.36.2`: local `kube-apiserver` and `etcd`.
- `controller-runtime v0.24.1`, as pinned by Flux Operator's own `go.mod`.
- The real `GitRepository` CRD from the pinned operator's Flux `v2.7.0` source-controller fixture.

The same unmodified consumer fixture passed again after the [JSON v2 migration](jsonv2-migration.md) on 2026-09-09. The bridge used the migrated service and HTTP serializer. All creation, 503 preservation, rename, removal and successful empty-result assertions passed; the package completed in 7.495 seconds.

After the [prepared configuration refactor](verification.md#prepared-configuration-refactor), the unchanged consumer assertions passed again on 2026-09-09 in 7.680 seconds. Only the bridge's configuration construction changed to `config.Parse`, `config.Prepare` and `service.New`; its scenarios still inject catalogs into the real service. The bridge also passed its build-tag-specific lint check with 0 issues.

The test uses the unmodified upstream `ResourceSetInputProviderReconciler` and `ResourceSetReconciler`, a real isolated Kubernetes API server, server-side apply and Flux inventory garbage collection. It performs explicit reconciliation calls so no background timing determines the result. No source-controller is started, so Git cloning and artifact readiness are outside this test.

The service side runs the project's real `service.New`, `Service.Scan`, filter evaluation, generation publication and `httpapi.New` in a separate [test bridge](../integration/flux/bridge/main.go). Flux requests the actual handler over loopback HTTP. The bridge is built with `-race`; the test injects only source catalogs or source failures through a fake Scanner. No HTTP success or error response is stubbed.

The verified path is `fake GitHub catalog -> real service/filter -> real HTTP handler -> Flux ResourceSetInputProvider -> Flux ResourceSet -> real GitRepository resources`. The command entrypoint, scheduler timing, GitHub API, Kubernetes networking and deployment remain outside this integration scenario.

| HTTP response | Observed Kubernetes behavior |
| --- | --- |
| Initial `503` before any scan | Real handler reports unavailability without an `inputs` field |
| `200`, two matching repository inputs | Both `GitRepository` resources created; archived, fork and unmatched-topic repositories excluded |
| `503` after a source failure carrying a partial catalog | Real handler returns a structured error without `inputs`; provider becomes `NotReady`; both resources retain their UIDs after another ResourceSet reconciliation |
| `200`, one repository renamed and the other's topic removed | Existing resource URL updated under the same Kubernetes name and UID; repository that no longer matches deleted |
| `200`, `{"inputs":[]}` after all repositories lose the required topic | Remaining resource deleted |
| `200`, `{"inputs":[]}` for a genuinely empty source catalog | Provider remains ready; resources remain absent |

Flux retains the provider's last successful `status.exportedInputs` when the HTTP request fails. ResourceSet can continue reading those inputs even while the provider is `NotReady`. This explains resource preservation in this version. It does not require this service to return stale HTTP data. A successful empty response has deliberately different semantics and invokes garbage collection.

The consumer scenario was designed independently of the service implementation in [contract_test.go](../integration/flux/testdata/contract_test.go), initially against an HTTP fixture. Once the project service was implemented, the fixture was replaced with the real service bridge while preserving the creation, error preservation, update and deletion expectations. Every actual HTTP response is also checked for `Cache-Control: no-store`; errors must have an `error` object and no `inputs`; empty successes must encode `[]`, not `null`.

## Reproduce

From the repository root:

```sh
mise install
mise exec -- bash integration/flux/run.sh
```

The first execution needs network access to download the pinned upstream source, envtest release archive and upstream Go dependencies. It also needs permission to start loopback listeners and a C compiler for Go's race detector. Supported release assets are Linux/macOS on amd64/arm64; SHA512 values are pinned in the script. Python 3, Git, curl and tar are used only for test setup.

The runner verifies Flux's commit and rejects modifications to tracked upstream files. It builds the bridge from this project's own module and exposes its path through `FRD_CONTRACT_BRIDGE`. It then overlays our test into the upstream test package through Go's `-overlay` option, leaving upstream files unchanged. The fixture lives under `testdata`, which Go excludes from package discovery and `go mod tidy`; Kubernetes dependencies stay in the cached upstream Go module. The production module gains no Kubernetes or Flux dependency. The `fluxcontract` build tag keeps the bridge outside normal runtime builds.

The runner explicitly sets `USE_EXISTING_CLUSTER=false`, supplies envtest binaries and replaces `KUBECONFIG` with an unused path. The upstream suite generates a new kubeconfig from its local test API server. Neither a user's Kubernetes context nor an existing cluster is used. Test cleanup closes the bridge's stdin and waits for its HTTP server to stop; the upstream suite stops its manager, API server and etcd. A process check after the recorded run found none of these test processes still running.

The command that executes the scenario, after setup, is:

```sh
go test -count=1 -timeout=5m -tags=fluxcontract \
  -overlay="$PROJECT/.cache/flux-contract-overlay.json" \
  ./internal/controller -run '^TestFRDExternalServiceContract$' -v
```

This command runs inside `$PROJECT/.cache/flux-operator`; the wrapper supplies the environment. Full output is written to `.cache/flux-contract-result.log`.

Observed result:

```text
200 with two inputs: two GitRepositories created
503: provider NotReady; both GitRepositories preserved
200 with one renamed input: same GitRepository name updated; removed input deleted
200 with inputs=[]: remaining GitRepository deleted
200 with empty source catalog: empty successful output remains valid
--- PASS: TestFRDExternalServiceContract (3.74s)
PASS
Stopping the test environment
ok github.com/controlplaneio-fluxcd/flux-operator/internal/controller 7.310s
```

The bridge also passed the project's configured linters:

```sh
mise exec -- golangci-lint run --build-tags=fluxcontract ./integration/flux/bridge
# 0 issues.
```

`mise exec -- go mod tidy` also succeeds without discovering the overlay fixture. The resulting runtime module requires only `go.yaml.in/yaml/v3 v3.0.5`; `go.sum` contains its two checksum entries.

## Pinned upstream references

- [ResourceSetInputProvider reconciler](https://github.com/controlplaneio-fluxcd/flux-operator/blob/e0f71db839116724c84caf8e06ab8423e7d9b174/internal/controller/resourcesetinputprovider_controller.go) retains prior exported inputs on external service failure.
- [ResourceSetInputProvider.GetInputs](https://github.com/controlplaneio-fluxcd/flux-operator/blob/e0f71db839116724c84caf8e06ab8423e7d9b174/api/v1/resourcesetinputprovider_types.go) returns exported inputs from status.
- [ResourceSet reconciler](https://github.com/controlplaneio-fluxcd/flux-operator/blob/e0f71db839116724c84caf8e06ab8423e7d9b174/internal/controller/resourceset_controller.go) reconciles an empty set when providers successfully return no inputs.
- [Kubernetes envtest v1.36.2 release](https://github.com/kubernetes-sigs/controller-tools/releases/tag/envtest-v1.36.2).

//go:build fluxcontract

// This independent consumer contract is compiled into the pinned upstream
// controller test package by run.sh using a Go overlay. The runtime module does
// not depend on Kubernetes or Flux libraries.
package controller

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fluxcd/pkg/runtime/conditions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	fluxv1 "github.com/controlplaneio-fluxcd/flux-operator/api/v1"
)

type frdRepository struct {
	ID       int64    `json:"id"`
	Owner    string   `json:"owner"`
	Name     string   `json:"name"`
	Topics   []string `json:"topics"`
	Archived bool     `json:"archived"`
	Fork     bool     `json:"fork"`
}

type frdBridge struct {
	url    string
	input  *json.Encoder
	output *json.Decoder
}

type frdBridgeResponse struct {
	URL       string `json:"url"`
	ScanError bool   `json:"scanError"`
}

func frdStartBridge(t *testing.T) *frdBridge {
	t.Helper()
	binary := os.Getenv("FRD_CONTRACT_BRIDGE")
	if binary == "" {
		t.Fatal("FRD_CONTRACT_BRIDGE must point to the real service test bridge; run integration/flux/run.sh")
	}
	command := exec.Command(binary)
	command.Stderr = os.Stderr
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() {
		_ = stdin.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("real service bridge exited: %v", err)
			}
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			<-done
			t.Error("real service bridge did not stop after stdin closed")
		}
	})
	bridge := &frdBridge{input: json.NewEncoder(stdin), output: json.NewDecoder(stdout)}
	response := bridge.receive(t)
	if !strings.HasPrefix(response.URL, "http://127.0.0.1:") {
		t.Fatalf("bridge did not start a loopback HTTP service: %+v", response)
	}
	bridge.url = response.URL
	return bridge
}

func (b *frdBridge) receive(t *testing.T) frdBridgeResponse {
	t.Helper()
	var response frdBridgeResponse
	done := make(chan error, 1)
	go func() { done <- b.output.Decode(&response) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("read real service bridge response: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("real service bridge response timed out")
	}
	return response
}

func (b *frdBridge) scan(t *testing.T, fail bool, repositories ...frdRepository) {
	t.Helper()
	request := struct {
		Repositories []frdRepository `json:"repositories"`
		Fail         bool            `json:"fail"`
	}{Repositories: repositories, Fail: fail}
	if err := b.input.Encode(request); err != nil {
		t.Fatalf("send test source catalog: %v", err)
	}
	if response := b.receive(t); response.ScanError != fail {
		t.Fatalf("real Service.Scan error = %v, want %v", response.ScanError, fail)
	}
}

func (b *frdBridge) assertHTTP(t *testing.T, ctx context.Context, status int, wantEmpty bool) {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, b.url+"/inputs/applications", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("real handler HTTP=%d, cache=%q, body=%s", response.StatusCode, response.Header.Get("Cache-Control"), body)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK && (payload["inputs"] != nil || payload["error"] == nil) {
		t.Fatalf("real handler error masquerades as a success: %s", body)
	}
	if wantEmpty && strings.TrimSpace(string(body)) != `{"inputs":[]}` {
		t.Fatalf("real handler empty response = %s", body)
	}
}

// TestFRDExternalServiceContract deliberately tests public Kubernetes behavior,
// not HTTP parsing alone. Reconciliation uses the unmodified upstream code and
// real API server persistence, server-side apply, inventory and garbage collection.
func TestFRDExternalServiceContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	frdInstallGitRepositoryCRD(t)

	namespace, err := testEnv.CreateNamespace(ctx, "frd-contract")
	if err != nil {
		t.Fatal(err)
	}
	bridge := frdStartBridge(t)
	bridge.assertHTTP(t, ctx, http.StatusServiceUnavailable, false)
	first := frdRepository{ID: 101, Owner: "acme", Name: "api", Topics: []string{"gitops"}}
	second := frdRepository{ID: 202, Owner: "acme", Name: "worker", Topics: []string{"gitops"}}
	bridge.scan(t, false, first, second,
		frdRepository{ID: 303, Owner: "acme", Name: "archived", Topics: []string{"gitops"}, Archived: true},
		frdRepository{ID: 404, Owner: "acme", Name: "fork", Topics: []string{"gitops"}, Fork: true},
		frdRepository{ID: 505, Owner: "acme", Name: "unmanaged", Topics: []string{"other"}},
	)
	bridge.assertHTTP(t, ctx, http.StatusOK, false)

	provider := &fluxv1.ResourceSetInputProvider{
		ObjectMeta: metav1.ObjectMeta{Name: "applications", Namespace: namespace.Name},
		Spec: fluxv1.ResourceSetInputProviderSpec{
			Type: "ExternalService", URL: bridge.url + "/inputs/applications", Insecure: true,
		},
	}
	if err := testClient.Create(ctx, provider); err != nil {
		t.Fatal(err)
	}
	resources := &fluxv1.ResourceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "applications", Namespace: namespace.Name},
		Spec: fluxv1.ResourceSetSpec{
			InputsFrom: []fluxv1.InputProviderReference{{Name: provider.Name}},
			ResourcesTemplate: `apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: repo-<< inputs.id >>
  namespace: << inputs.provider.namespace >>
spec:
  interval: 10m
  url: https://github.com/<< inputs.fullName >>.git
`,
		},
	}
	if err := testClient.Create(ctx, resources); err != nil {
		t.Fatal(err)
	}
	providerReconciler := getResourceSetInputProviderReconciler(t)
	resourceReconciler := getResourceSetReconciler(t)
	providerKey := client.ObjectKeyFromObject(provider)
	resourceKey := client.ObjectKeyFromObject(resources)
	reconcileProvider := func(wantError bool) {
		t.Helper()
		_, reconcileErr := providerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: providerKey})
		if (reconcileErr != nil) != wantError {
			t.Fatalf("provider reconcile error = %v, want error = %v", reconcileErr, wantError)
		}
	}
	reconcileResources := func() {
		t.Helper()
		if _, err := resourceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: resourceKey}); err != nil {
			t.Fatal(err)
		}
	}
	assertReadyProvider := func(want bool, count int) {
		t.Helper()
		observed := &fluxv1.ResourceSetInputProvider{}
		if err := testClient.Get(ctx, providerKey, observed); err != nil {
			t.Fatal(err)
		}
		if conditions.IsReady(observed) != want || len(observed.Status.ExportedInputs) != count {
			t.Fatalf("provider ready=%v, exported=%d, want ready=%v, exported=%d; status=%+v",
				conditions.IsReady(observed), len(observed.Status.ExportedInputs), want, count, observed.Status)
		}
	}
	assertRepositories := func(want map[string]string) {
		t.Helper()
		list := &unstructured.UnstructuredList{}
		list.SetAPIVersion("source.toolkit.fluxcd.io/v1")
		list.SetKind("GitRepositoryList")
		if err := testClient.List(ctx, list, client.InNamespace(namespace.Name)); err != nil {
			t.Fatal(err)
		}
		got := make(map[string]string, len(list.Items))
		for _, item := range list.Items {
			url, found, err := unstructured.NestedString(item.Object, "spec", "url")
			if err != nil || !found || item.GetDeletionTimestamp() != nil {
				t.Fatalf("invalid or deleting repository: %+v, error=%v", item.Object, err)
			}
			got[item.GetName()] = url
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("GitRepositories = %v, want %v", got, want)
		}
	}
	repositoryUID := func(name string) types.UID {
		t.Helper()
		obj := &unstructured.Unstructured{}
		obj.SetAPIVersion("source.toolkit.fluxcd.io/v1")
		obj.SetKind("GitRepository")
		if err := testClient.Get(ctx, client.ObjectKey{Namespace: namespace.Name, Name: name}, obj); err != nil {
			t.Fatal(err)
		}
		return obj.GetUID()
	}

	// The first reconciliation installs finalizers, the next performs discovery.
	reconcileProvider(false)
	reconcileProvider(false)
	assertReadyProvider(true, 2)
	reconcileResources()
	reconcileResources()
	wantInitial := map[string]string{
		"repo-101": "https://github.com/acme/api.git",
		"repo-202": "https://github.com/acme/worker.git",
	}
	assertRepositories(wantInitial)
	firstUID, secondUID := repositoryUID("repo-101"), repositoryUID("repo-202")
	t.Log("200 with two inputs: two GitRepositories created")

	bridge.scan(t, true, frdRepository{ID: 606, Owner: "acme", Name: "partial", Topics: []string{"gitops"}})
	bridge.assertHTTP(t, ctx, http.StatusServiceUnavailable, false)
	reconcileProvider(true)
	assertReadyProvider(false, 2)
	// A direct reconciliation while the provider is failing must preserve all
	// resources. Whether the ResourceSet reports an error is Flux's own policy.
	_, _ = resourceReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: resourceKey})
	assertRepositories(wantInitial)
	if repositoryUID("repo-101") != firstUID || repositoryUID("repo-202") != secondUID {
		t.Fatal("HTTP failure replaced a previously created repository")
	}
	t.Log("503: provider NotReady; both GitRepositories preserved")

	first.Name = "api-renamed"
	second.Topics = []string{"unmanaged"}
	bridge.scan(t, false, first, second)
	bridge.assertHTTP(t, ctx, http.StatusOK, false)
	reconcileProvider(false)
	assertReadyProvider(true, 1)
	reconcileResources()
	assertRepositories(map[string]string{"repo-101": "https://github.com/acme/api-renamed.git"})
	if repositoryUID("repo-101") != firstUID {
		t.Fatal("renaming a repository replaced its Kubernetes resource")
	}
	t.Log("200 with one renamed input: same GitRepository name updated; removed input deleted")

	first.Topics = []string{"unmanaged"}
	bridge.scan(t, false, first, second)
	bridge.assertHTTP(t, ctx, http.StatusOK, true)
	reconcileProvider(false)
	assertReadyProvider(true, 0)
	reconcileResources()
	assertRepositories(map[string]string{})
	t.Log("200 with inputs=[]: remaining GitRepository deleted")
	bridge.scan(t, false)
	bridge.assertHTTP(t, ctx, http.StatusOK, true)
	reconcileProvider(false)
	assertReadyProvider(true, 0)
	reconcileResources()
	assertRepositories(map[string]string{})
	t.Log("200 with empty source catalog: empty successful output remains valid")
}

func frdInstallGitRepositoryCRD(t *testing.T) {
	t.Helper()
	path := filepath.Join("..", "builder", "testdata", "v2.7.0", "source-controller.yaml")
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader := kubeyaml.NewYAMLReader(bufio.NewReader(file))
	for {
		document, err := reader.Read()
		if errors.Is(err, io.EOF) {
			t.Fatal("upstream fixture has no GitRepository CRD")
		}
		if err != nil {
			t.Fatal(err)
		}
		var crd apiextensionsv1.CustomResourceDefinition
		if err := yaml.Unmarshal(document, &crd); err != nil {
			t.Fatal(err)
		}
		if crd.Name != "gitrepositories.source.toolkit.fluxcd.io" {
			continue
		}
		_, err = envtest.InstallCRDs(testEnv.Config, envtest.CRDInstallOptions{
			CRDs: []*apiextensionsv1.CustomResourceDefinition{&crd},
		})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			t.Fatal(err)
		}
		return
	}
}

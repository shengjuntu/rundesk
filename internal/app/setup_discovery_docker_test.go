package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type discoveryDockerFixture struct {
	records     []setupDockerContainer
	calls       [][]string
	unavailable bool
}

func (d *discoveryDockerFixture) Command(args ...string) *exec.Cmd {
	panic("discovery must not execute containers")
}
func (d *discoveryDockerFixture) Run(ctx context.Context, args ...string) ([]byte, error) {
	d.calls = append(d.calls, append([]string{}, args...))
	if d.unavailable {
		return nil, fmt.Errorf("fixture engine unavailable")
	}
	if reflect.DeepEqual(args, []string{"container", "ls", "--filter", "status=running", "--no-trunc", "--format", "{{.ID}}"}) {
		ids := []string{}
		for _, c := range d.records {
			ids = append(ids, c.ID)
		}
		return []byte(strings.Join(ids, "\n")), nil
	}
	if len(args) > 4 && reflect.DeepEqual(args[:4], []string{"container", "inspect", "--format", setupDockerInspectFormat}) {
		lines := []string{}
		for _, id := range args[4:] {
			for _, c := range d.records {
				if c.ID == id {
					raw, _ := json.Marshal(c)
					lines = append(lines, string(raw))
				}
			}
		}
		return []byte(strings.Join(lines, "\n")), nil
	}
	return nil, fmt.Errorf("unexpected Docker mutation or command: %q", args)
}
func discoveryContainer(id int, name string) setupDockerContainer {
	return setupDockerContainer{ID: fmt.Sprintf("%064x", id), Name: "/" + name, Image: "vllm/vllm-openai:fixture", NetworkMode: "bridge", Running: true, Ports: map[string][]setupDockerPortBinding{}}
}

func TestSetupDockerDiscoveryPublishedPortsAndActualModelsProbe(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/models" || r.Method != "GET" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{"data":[{"id":"docker-vllm-model"}]}`)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	c := discoveryContainer(1, "vllm")
	c.Ports["8000/tcp"] = []setupDockerPortBinding{{HostIP: u.Hostname(), HostPort: u.Port()}, {HostIP: u.Hostname(), HostPort: u.Port()}}
	c.Ports["9000/udp"] = []setupDockerPortBinding{{HostIP: "0.0.0.0", HostPort: "19999"}}
	driver := &discoveryDockerFixture{records: []setupDockerContainer{c}}
	endpoints, inventory, warnings := setupDockerDiscovery(context.Background(), driver, false, "")
	endpoint := server.URL + "/v1"
	if len(endpoints) != 1 || len(endpoints[endpoint]) != 1 || len(inventory) != 1 || inventory[0].Name != "vllm" || inventory[0].Status != "published" || len(warnings) != 0 {
		t.Fatal(endpoints, inventory, warnings)
	}
	client := setupDockerProbeClient(time.Second, endpoints)
	defer client.CloseIdleConnections()
	service := setupDiscoverService(context.Background(), client, endpoint, "docker", "")
	if service.Status != "listed" || len(service.Models) != 1 || service.Models[0].ID != "docker-vllm-model" || service.InferenceTested || calls != 1 {
		t.Fatal(service, calls)
	}
	if strings.Contains(setupDockerInspectFormat, ".Config.Env") || strings.Contains(setupDockerInspectFormat, ".Config.Cmd") || strings.Contains(setupDockerInspectFormat, ".NetworkSettings.Networks") {
		t.Fatal("excessive Docker inventory")
	}
}

func TestSetupDockerDiscoveryBindingAddressesAndNamespaces(t *testing.T) {
	cases := []struct {
		binding       setupDockerPortBinding
		inside        bool
		gateway, want string
	}{
		{setupDockerPortBinding{"0.0.0.0", "18000"}, false, "", "http://127.0.0.1:18000/v1"},
		{setupDockerPortBinding{"::", "18000"}, false, "", "http://[::1]:18000/v1"},
		{setupDockerPortBinding{"192.168.0.234", "18000"}, false, "", "http://192.168.0.234:18000/v1"},
		{setupDockerPortBinding{"127.0.0.2", "18000"}, false, "", "http://127.0.0.2:18000/v1"},
		{setupDockerPortBinding{"0.0.0.0", "18000"}, true, "host.docker.internal", "http://host.docker.internal:18000/v1"},
		{setupDockerPortBinding{"0.0.0.0", "18000"}, true, "", ""},
		{setupDockerPortBinding{"127.0.0.1", "18000"}, true, "host.docker.internal", ""},
		{setupDockerPortBinding{"bad-host", "18000"}, false, "", ""},
		{setupDockerPortBinding{"0.0.0.0", "0"}, false, "", ""},
	}
	for _, tc := range cases {
		got, reason := setupDockerBindingEndpoint(tc.binding, tc.inside, tc.gateway)
		if got != tc.want || (got == "") == (reason == "") {
			t.Fatal(tc, got, reason)
		}
	}
}

func TestSetupDockerDiscoveryUnexposedAndHostInventoryAreNotModels(t *testing.T) {
	hidden := discoveryContainer(1, "private-vllm")
	hidden.Ports["8000/tcp"] = nil
	host := discoveryContainer(2, "host-vllm")
	host.NetworkMode = "host"
	off := discoveryContainer(3, "isolated")
	off.NetworkMode = "none"
	driver := &discoveryDockerFixture{records: []setupDockerContainer{hidden, host, off}}
	bindings, rows, warnings := setupDockerDiscovery(context.Background(), driver, false, "")
	if len(bindings) != 0 || len(rows) != 3 || len(warnings) != 0 {
		t.Fatal(bindings, rows, warnings)
	}
	statuses := map[string]string{}
	for _, row := range rows {
		statuses[row.Name] = row.Status
		if row.Warning == "" || len(row.Endpoints) != 0 {
			t.Fatal(row)
		}
	}
	if statuses["private-vllm"] != "unexposed" || statuses["host-vllm"] != "host" || statuses["isolated"] != "unsupported" {
		t.Fatal(statuses)
	}
}

func TestSetupDockerDiscoveryBoundedAndUnavailable(t *testing.T) {
	driver := &discoveryDockerFixture{}
	for n := 1; n <= 40; n++ {
		c := discoveryContainer(n, "candidate-"+strconv.Itoa(n))
		c.Ports["8000/tcp"] = []setupDockerPortBinding{{"0.0.0.0", strconv.Itoa(18000 + n)}, {"0.0.0.0", strconv.Itoa(19000 + n)}}
		driver.records = append(driver.records, c)
	}
	bindings, rows, warnings := setupDockerDiscovery(context.Background(), driver, false, "")
	if len(bindings) != 32 || len(rows) != 32 || len(warnings) == 0 || len(driver.calls) != 2 || len(driver.calls[1]) != 36 {
		t.Fatal(len(bindings), len(rows), warnings, driver.calls)
	}
	driver.unavailable = true
	bindings, rows, warnings = setupDockerDiscovery(context.Background(), driver, false, "")
	if len(bindings) != 0 || len(rows) != 0 || len(warnings) != 1 {
		t.Fatal(bindings, rows, warnings)
	}
}

func TestSetupDockerFailedCandidatesRemainVisible(t *testing.T) {
	for _, state := range []string{"unreachable", "incompatible", "auth_required", "empty", "listed"} {
		service := SetupDiscoveryService{Endpoint: "http://127.0.0.1:18000/v1", Status: state, ContainerIDs: []string{"container"}}
		if !setupRetainDiscoveryService(service, false) {
			t.Fatal("Docker failure disappeared", state)
		}
		service.ContainerIDs = nil
		if !setupRetainDiscoveryService(service, true) {
			t.Fatal("explicit failure disappeared", state)
		}
	}
}

func TestSetupDockerKnownBindingsBypassProxyOnlyForExactModelsRequest(t *testing.T) {
	client := setupDockerProbeClient(time.Second, map[string][]string{"http://192.168.0.234:18000/v1": {"fixture"}})
	defer client.CloseIdleConnections()
	transport := client.Transport.(*http.Transport)
	req, _ := http.NewRequest("GET", "http://192.168.0.234:18000/v1/models", nil)
	if proxy, err := transport.Proxy(req); err != nil || proxy != nil {
		t.Fatal(proxy, err)
	}
}

// Exercise the complete production scan rather than granting the credential
// to every port candidate when a user supplies one authenticated service.
func TestSetupDiscoveryCredentialsStayOnExplicitEndpoint(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:8000")
	if err != nil {
		t.Skip("common automatic discovery fixture port unavailable")
	}
	var leaked, automaticCalls atomic.Int32
	automatic := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		automaticCalls.Add(1)
		if r.Header.Get("Authorization") != "" {
			leaked.Add(1)
		}
		fmt.Fprint(w, `{"data":[{"id":"automatic"}]}`)
	}))
	automatic.Listener = listener
	automatic.Start()
	defer automatic.Close()
	const secret = "test-discovery-credential-only"
	explicit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("explicit credential missing")
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer explicit.Close()
	t.Setenv("RUNDESK_DISCOVERY_CREDENTIAL_FIXTURE", secret)
	m := testManager(t)
	m.docker = &discoveryDockerFixture{}
	result, err := m.discoverSetup(context.Background(), SetupDiscoveryInput{Endpoints: []string{explicit.URL + "/v1"}, APIKeyEnv: "RUNDESK_DISCOVERY_CREDENTIAL_FIXTURE"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, service := range result.Services {
		if service.Endpoint == explicit.URL+"/v1" {
			found = true
			if service.Source != "explicit" || service.Status != "incompatible" {
				t.Fatal(service)
			}
		}
	}
	if !found || automaticCalls.Load() == 0 || leaked.Load() != 0 {
		t.Fatal("missing explicit failure or leaked automatic credential", found, automaticCalls.Load(), leaked.Load())
	}
}

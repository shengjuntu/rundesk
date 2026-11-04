package app

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// This is inventory, not a claim that a container is a model server. Only a
// successful models request can promote one of its endpoint candidates.
type SetupDiscoveryContainer struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Image       string   `json:"image"`
	NetworkMode string   `json:"networkMode"`
	Status      string   `json:"status"`
	Warning     string   `json:"warning,omitempty"`
	Endpoints   []string `json:"endpoints"`
}

type setupDockerPortBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}
type setupDockerContainer struct {
	ID, Name, Image, NetworkMode string
	Running                      bool
	Ports                        map[string][]setupDockerPortBinding
}

// Explicit projection prevents environment variables, commands, labels and
// credentials from entering discovery output or diagnostics.
const setupDockerInspectFormat = `{"id":{{json .Id}},"name":{{json .Name}},"image":{{json .Config.Image}},"networkMode":{{json .HostConfig.NetworkMode}},"running":{{json .State.Running}},"ports":{{json .NetworkSettings.Ports}}}`

var setupDockerContainerID = regexp.MustCompile(`^[a-f0-9]{12,64}$`)

func setupInsideContainer() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

func setupDockerGateway(ctx context.Context, inside bool) string {
	if !inside {
		return ""
	}
	probe, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if hosts, err := net.DefaultResolver.LookupHost(probe, "host.docker.internal"); err == nil && len(hosts) > 0 {
		return "host.docker.internal"
	}
	return ""
}

// A Docker-published address is a known binding of the configured local Engine,
// not a port-range or LAN scan. Never substitute a container's private IP.
func setupDockerBindingEndpoint(binding setupDockerPortBinding, inside bool, gateway string) (string, string) {
	port, err := strconv.Atoi(binding.HostPort)
	if err != nil || port < 1 || port > 65535 {
		return "", "Docker 返回了无效的宿主端口"
	}
	host := binding.HostIP
	if host == "" || host == "0.0.0.0" || host == "::" {
		if inside {
			if gateway == "" {
				return "", "RunDesk 位于容器中，无法确认宿主访问地址；请配置 host.docker.internal 或手工填写宿主模型地址"
			}
			host = gateway
		} else if host == "::" {
			host = "::1"
		} else {
			host = "127.0.0.1"
		}
	} else {
		ip := net.ParseIP(host)
		if ip == nil || ip.IsMulticast() || ip.IsUnspecified() {
			return "", "Docker 返回了无效的发布地址"
		}
		if inside && ip.IsLoopback() {
			return "", "此端口仅绑定 Docker 宿主回环地址；RunDesk 的容器网络是否共享宿主尚未确认，请手工填写并测试可达地址"
		}
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/v1", ""
}

func setupDockerDiscovery(ctx context.Context, driver dockerDriver, inside bool, gateway string) (map[string][]string, []SetupDiscoveryContainer, []string) {
	bindings := map[string][]string{}
	containers := []SetupDiscoveryContainer{}
	warnings := []string{}
	if driver == nil {
		return bindings, containers, []string{"Docker 模型发现不可用；可手工填写服务地址"}
	}
	probe, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	out, err := driver.Run(probe, "container", "ls", "--filter", "status=running", "--no-trunc", "--format", "{{.ID}}")
	if err != nil {
		return bindings, containers, []string{"无法读取本机 Docker 容器；请检查 Docker 服务与 RunDesk 运行账户的访问权限，仍可手工填写模型地址"}
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, id := range strings.Fields(string(out)) {
		if !setupDockerContainerID.MatchString(id) {
			warnings = append(warnings, "Docker 容器清单含无效编号，已跳过")
			continue
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > 32 {
		ids = ids[:32]
		warnings = append(warnings, "Docker 发现本次最多读取 32 个运行中容器；其余服务可手工填写地址")
	}
	if len(ids) == 0 {
		return bindings, containers, warnings
	}
	selectedIDs := map[string]bool{}
	for _, id := range ids {
		selectedIDs[id] = true
	}
	args := append([]string{"container", "inspect", "--format", setupDockerInspectFormat}, ids...)
	out, inspectErr := driver.Run(probe, args...)
	if inspectErr != nil {
		warnings = append(warnings, "部分 Docker 容器信息无法读取，可能已停止或访问超时；请重新扫描")
	}
	inspected := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	scanner.Buffer(make([]byte, 4096), 128<<10)
	for scanner.Scan() {
		var raw setupDockerContainer
		if json.Unmarshal(scanner.Bytes(), &raw) != nil || !selectedIDs[raw.ID] {
			warnings = append(warnings, "Docker 容器网络信息格式不完整，已跳过")
			continue
		}
		if inspected[raw.ID] {
			continue
		}
		inspected[raw.ID] = true
		if !raw.Running { // Do not probe stale bindings of a container that stopped.
			continue
		}
		c := SetupDiscoveryContainer{ID: raw.ID, Name: strings.TrimPrefix(raw.Name, "/"), Image: raw.Image, NetworkMode: raw.NetworkMode, Status: "unexposed", Endpoints: []string{}}
		if len(c.Name) > 160 {
			c.Name = c.Name[:160]
		}
		if len(c.Image) > 512 {
			c.Image = c.Image[:512]
		}
		if len(c.NetworkMode) > 160 {
			c.NetworkMode = c.NetworkMode[:160]
		}
		if raw.NetworkMode == "host" {
			c.Status = "host"
			c.Warning = "使用宿主网络；由宿主监听端口扫描探测，不能仅凭容器名称确定模型接口"
			if inside {
				c.Warning = "使用 Docker 宿主网络；RunDesk 位于容器中，网络是否共享尚未确认，请手工填写并测试宿主模型地址"
			}
			containers = append(containers, c)
			continue
		}
		keys := []string{}
		for key := range raw.Ports {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		published := false
		for _, key := range keys {
			if !strings.HasSuffix(key, "/tcp") {
				continue
			}
			for _, binding := range raw.Ports[key] {
				published = true
				endpoint, reason := setupDockerBindingEndpoint(binding, inside, gateway)
				if reason != "" {
					c.Warning = reason
					continue
				}
				if _, exists := bindings[endpoint]; !exists && len(bindings) >= 32 {
					c.Warning = "Docker 模型发现达到 32 个候选地址上限；可手工填写其余服务地址"
					continue
				}
				if !setupDiscoveryContains(c.Endpoints, endpoint) {
					c.Endpoints = append(c.Endpoints, endpoint)
					bindings[endpoint] = append(bindings[endpoint], c.ID)
				}
			}
		}
		if published {
			c.Status = "published"
		} else {
			c.Warning = "没有发布 TCP 端口；容器内部端口不会自动作为宿主可访问地址，请发布端口或手工填写可达地址"
		}
		if raw.NetworkMode == "none" {
			c.Status, c.Warning = "unsupported", "容器禁用网络，无法作为外部模型服务访问"
		}
		containers = append(containers, c)
	}
	if scanner.Err() != nil {
		warnings = append(warnings, "Docker 网络信息超出读取上限或读取中断")
	}
	for _, id := range ids {
		if !inspected[id] {
			containers = append(containers, SetupDiscoveryContainer{ID: id, Name: id[:12], Status: "inspect_failed", Warning: "无法读取该容器的网络信息，请重新扫描", Endpoints: []string{}})
		}
	}
	sort.Slice(containers, func(i, j int) bool { return containers[i].Name < containers[j].Name })
	return bindings, containers, warnings
}

func setupDiscoveryContains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// Keep failed explicit and Docker candidates visible, including an overall
// timeout before their HTTP request started. A partial scan is not absence.
func setupRetainDiscoveryService(service SetupDiscoveryService, explicit bool) bool {
	return explicit || len(service.ContainerIDs) > 0 || service.Status == "listed" || service.Status == "empty" || service.AuthRequired
}

func setupDockerProbeClient(timeout time.Duration, bindings map[string][]string) *http.Client {
	client := setupProbeClient(timeout)
	transport := client.Transport.(*http.Transport)
	fallback := transport.Proxy
	transport.Proxy = func(req *http.Request) (*url.URL, error) {
		// These exact destinations were published by the local Engine. Sending
		// their discovery requests to an external HTTP proxy can hide a perfectly
		// reachable local service. Other manually supplied endpoints retain the
		// configured proxy and NO_PROXY behavior.
		if len(bindings[strings.TrimSuffix(req.URL.String(), "/models")]) > 0 {
			return nil, nil
		}
		return fallback(req)
	}
	return client
}

package server

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	dockerHelperName  = "liking-self-upgrade"
	dockerAPIPrefix   = "/v1.41"
	githubReleaseRepo = "cheesydui-cloud/liking"
	maxReleaseBytes   = 128 << 20
	minReleaseBytes   = 1 << 20
	maxSumsBytes      = 1 << 20
	fallbackGHProxy   = "https://gh-proxy.com/"
)

var (
	dockerUpgradeSock     = "/var/run/docker.sock"
	dockerUpgradeDir      = "/opt/liking-docker"
	containerIDRe         = regexp.MustCompile(`^[0-9a-fA-F]{12,64}$`)
	containerNameRe       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	assetNameRe           = regexp.MustCompile(`^[A-Za-z0-9._-]{1,80}$`)
	dockerIDInText        = regexp.MustCompile(`(?:docker[-/]|containers/)([0-9a-f]{12,64})`)
	stageReleaseFn        = stageRelease
	sleepBeforeDockerStop = func() { time.Sleep(3 * time.Second) }
	dockerDial            = dialDocker
)

// RunDockerUpgrade downloads a release, rebuilds the local image, and replaces
// the panel container. It must run before the database is opened: the helper
// is a sibling container, and this process is that helper.
func RunDockerUpgrade(target, release string) error {
	log := openUpgradeLog(dockerUpgradeDir)
	defer log.close()
	api, err := dockerDial(dockerUpgradeSock)
	if err != nil {
		log.printf("连不上 Docker：%s", err.Error())
		return fmt.Errorf("连不上 Docker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	if err := runDockerUpgrade(ctx, target, release, dockerUpgradeDir, runtime.GOARCH, log, api); err != nil {
		log.printf("升级失败：%s", err.Error())
		return err
	}
	log.printf("升级完成")
	return nil
}

func runDockerUpgrade(ctx context.Context, target, release, dir, goarch string, log *upgradeLogger, api *dockerAPI) error {
	target = strings.TrimSpace(target)
	if !validContainerRef(target) {
		return fmt.Errorf("容器编号无效")
	}
	tag, err := resolveUpgradeTag(release)
	if err != nil {
		return err
	}
	log.printf("开始升级 %s %s", shortID(target), tag)
	ins, err := api.inspect(ctx, target)
	if err != nil {
		return fmt.Errorf("读不到当前容器：%s", err.Error())
	}
	if sameContainer(ins.ID, containerIDFromProcFiles()) || containerName(ins.Name) == dockerHelperName {
		return fmt.Errorf("拒绝替换升级容器自己")
	}
	if err := stageReleaseFn(ctx, tag, dir, goarch, log); err != nil {
		return err
	}
	if err := dockerfileReady(dir); err != nil {
		return err
	}
	tags := panelImageTags(ins)
	log.printf("构建镜像 %s", strings.Join(tags, " "))
	if err := api.buildImage(ctx, dir, tags, log); err != nil {
		return fmt.Errorf("构建失败，正在运行的容器没有被替换：%s", err.Error())
	}
	sleepBeforeDockerStop()
	if err := recreateContainer(ctx, api, ins, recreateImage(ins), log); err != nil {
		return err
	}
	return nil
}

func startDockerUpgrade(release string) error {
	if !dockerUpgradeReadyFn() {
		return fmt.Errorf("容器还没挂载 /var/run/docker.sock 和 /opt/liking-docker")
	}
	api, err := dockerDial(dockerUpgradeSock)
	if err != nil {
		return fmt.Errorf("连不上 Docker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	running, err := api.running(ctx, dockerHelperName)
	if err != nil {
		return fmt.Errorf("读不到升级状态：%s", err.Error())
	}
	if running {
		return fmt.Errorf("升级已在进行")
	}
	id, err := resolveSelfContainer(ctx, api)
	if err != nil {
		return err
	}
	ins, err := api.inspect(ctx, id)
	if err != nil {
		return fmt.Errorf("读不到当前容器：%s", err.Error())
	}
	if err := api.removeIfStopped(ctx, dockerHelperName); err != nil {
		return fmt.Errorf("清不掉上次的升级容器：%s", err.Error())
	}
	image := strings.TrimSpace(ins.Image)
	if image == "" {
		image = strings.TrimSpace(ins.Config.Image)
	}
	if image == "" {
		return fmt.Errorf("读不到当前镜像")
	}
	if err := api.createHelper(ctx, helperSpec{
		Image:   image,
		Bin:     panelBinary(ins),
		Target:  ins.ID,
		Release: strings.TrimSpace(release),
		Env:     proxyEnviron(),
		Binds: []string{
			dockerUpgradeSock + ":" + dockerUpgradeSock,
			dockerUpgradeDir + ":" + dockerUpgradeDir,
		},
	}); err != nil {
		return fmt.Errorf("启动升级失败：%s", err.Error())
	}
	appendUpgradeLog(dockerUpgradeDir, "已启动升级助手，目标 %s", shortID(ins.ID))
	return nil
}

func detectDockerUpgradeReady() bool {
	st, err := os.Stat(dockerUpgradeSock)
	if err != nil || st.IsDir() {
		return false
	}
	df, err := os.Stat(filepath.Join(dockerUpgradeDir, "Dockerfile"))
	return err == nil && !df.IsDir()
}

func detectPanelInContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	return containerIDFromProcFiles() != ""
}

func resolveSelfContainer(ctx context.Context, api *dockerAPI) (string, error) {
	if id := containerIDFromProcFiles(); id != "" {
		if ins, err := api.inspect(ctx, id); err == nil && ins.ID != "" {
			return ins.ID, nil
		}
	}
	host, _ := os.Hostname()
	if containerIDRe.MatchString(host) {
		if ins, err := api.inspect(ctx, host); err == nil && ins.ID != "" {
			return ins.ID, nil
		}
	}
	list, err := api.list(ctx)
	if err != nil {
		return "", fmt.Errorf("认不出当前容器")
	}
	var hits []dockerListItem
	for _, item := range list {
		if listItemIsPanel(item) {
			hits = append(hits, item)
		}
	}
	if len(hits) == 1 {
		return hits[0].ID, nil
	}
	return "", fmt.Errorf("认不出当前容器")
}

func containerIDFromProcFiles() string {
	var b strings.Builder
	for _, p := range []string{
		"/proc/self/mountinfo",
		"/proc/1/mountinfo",
		"/proc/self/cgroup",
		"/proc/1/cgroup",
		"/proc/self/cpuset",
		"/proc/1/cpuset",
	} {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	return containerIDFromText(b.String())
}

func containerIDFromText(text string) string {
	best := ""
	for _, m := range dockerIDInText.FindAllStringSubmatch(strings.ToLower(text), -1) {
		if len(m) == 2 && len(m[1]) > len(best) {
			best = m[1]
		}
	}
	return best
}

func listItemIsPanel(item dockerListItem) bool {
	data := false
	for _, m := range item.Mounts {
		if m.Destination == "/var/lib/liking" {
			data = true
		}
	}
	if !data {
		return false
	}
	for _, p := range item.Ports {
		if p.PrivatePort == 8899 {
			return true
		}
	}
	for _, n := range item.Names {
		if strings.TrimPrefix(n, "/") == "liking" {
			return true
		}
	}
	return strings.Contains(item.Image, "liking")
}

type helperSpec struct {
	Image   string
	Bin     string
	Target  string
	Release string
	Env     []string
	Binds   []string
}

func (d *dockerAPI) createHelper(ctx context.Context, spec helperSpec) error {
	if !validContainerRef(spec.Target) {
		return fmt.Errorf("容器编号无效")
	}
	bin := spec.Bin
	if bin == "" || !strings.HasPrefix(bin, "/") {
		bin = "/usr/local/bin/liking-server"
	}
	cmd := []string{"--docker-upgrade", "--docker-target", spec.Target}
	if spec.Release != "" {
		if !releaseTagRe.MatchString(spec.Release) {
			return fmt.Errorf("版本号无效")
		}
		cmd = append(cmd, "--docker-release", spec.Release)
	}
	body := map[string]any{
		"Image":      spec.Image,
		"Entrypoint": []string{bin},
		"Cmd":        cmd,
		"Env":        spec.Env,
		"HostConfig": map[string]any{
			"Binds":       spec.Binds,
			"NetworkMode": "bridge",
			"AutoRemove":  false,
		},
	}
	_, err := d.create(ctx, dockerHelperName, body)
	if err != nil {
		return err
	}
	return d.start(ctx, dockerHelperName)
}

func recreateContainer(ctx context.Context, api *dockerAPI, ins dockerInspect, image string, log *upgradeLogger) error {
	name := containerName(ins.Name)
	if name == "" || name == dockerHelperName || !containerNameRe.MatchString(name) {
		return fmt.Errorf("容器名称无效")
	}
	if sameContainer(ins.ID, containerIDFromProcFiles()) {
		return fmt.Errorf("拒绝替换升级容器自己")
	}
	body, err := recreateCreateBody(ins, image)
	if err != nil {
		return err
	}
	prev := name + "-previous"
	if len(prev) > 63 {
		prev = "liking-previous"
	}
	if err := api.removeIfStopped(ctx, prev); err != nil {
		return fmt.Errorf("清不掉上次留下的旧容器：%s", err.Error())
	}
	log.printf("停止 %s", name)
	if err := api.stop(ctx, ins.ID, 20); err != nil {
		return fmt.Errorf("停止容器失败：%s", err.Error())
	}
	if err := api.rename(ctx, ins.ID, prev); err != nil {
		_ = api.start(ctx, ins.ID)
		return fmt.Errorf("改名失败，已尝试拉回旧容器：%s", err.Error())
	}
	newID, err := api.create(ctx, name, body)
	if err != nil {
		_ = api.rename(ctx, ins.ID, name)
		_ = api.start(ctx, ins.ID)
		return fmt.Errorf("创建新容器失败，已尝试拉回旧容器：%s", err.Error())
	}
	if err := api.start(ctx, newID); err != nil {
		_ = api.removeForce(ctx, newID)
		_ = api.rename(ctx, ins.ID, name)
		_ = api.start(ctx, ins.ID)
		return fmt.Errorf("启动新容器失败，已尝试拉回旧容器：%s", err.Error())
	}
	if err := api.removeForce(ctx, prev); err != nil {
		log.printf("新容器已启动。旧容器 %s 还在：%s", prev, err.Error())
	}
	log.printf("已启动新容器 %s", name)
	return nil
}

type portBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

type restartPolicy struct {
	Name              string `json:"Name"`
	MaximumRetryCount int    `json:"MaximumRetryCount,omitempty"`
}

type dockerMount struct {
	Type        string `json:"Type"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	RW          bool   `json:"RW"`
	Mode        string `json:"Mode"`
}

type dockerInspect struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Image  string `json:"Image"`
	Config struct {
		Hostname     string              `json:"Hostname"`
		User         string              `json:"User"`
		Env          []string            `json:"Env"`
		Cmd          []string            `json:"Cmd"`
		Image        string              `json:"Image"`
		Entrypoint   []string            `json:"Entrypoint"`
		WorkingDir   string              `json:"WorkingDir"`
		Labels       map[string]string   `json:"Labels"`
		ExposedPorts map[string]struct{} `json:"ExposedPorts"`
		StopSignal   string              `json:"StopSignal"`
	} `json:"Config"`
	HostConfig struct {
		Binds          []string                 `json:"Binds"`
		NetworkMode    string                   `json:"NetworkMode"`
		PortBindings   map[string][]portBinding `json:"PortBindings"`
		RestartPolicy  restartPolicy            `json:"RestartPolicy"`
		Privileged     bool                     `json:"Privileged"`
		ExtraHosts     []string                 `json:"ExtraHosts"`
		DNS            []string                 `json:"Dns"`
		DNSSearch      []string                 `json:"DnsSearch"`
		ReadonlyRootfs bool                     `json:"ReadonlyRootfs"`
	} `json:"HostConfig"`
	Mounts []dockerMount `json:"Mounts"`
	State  struct {
		Running bool   `json:"Running"`
		Status  string `json:"Status"`
	} `json:"State"`
	NetworkSettings struct {
		Networks map[string]struct {
			Aliases []string `json:"Aliases"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

type dockerListItem struct {
	ID    string `json:"Id"`
	Names []string
	Image string
	Ports []struct {
		PrivatePort int    `json:"PrivatePort"`
		PublicPort  int    `json:"PublicPort"`
		Type        string `json:"Type"`
	}
	Mounts []dockerMount
	State  string
}

func recreateCreateBody(ins dockerInspect, image string) (map[string]any, error) {
	image = strings.TrimSpace(image)
	if image == "" || strings.ContainsAny(image, " \t\r\n") {
		return nil, fmt.Errorf("镜像名无效")
	}
	binds := append([]string{}, ins.HostConfig.Binds...)
	if len(binds) == 0 {
		binds = bindsFromMounts(ins.Mounts)
	}
	if !bindsKeepData(binds) {
		return nil, fmt.Errorf("克隆配置里没有数据目录，已中止")
	}
	if !commandKeepsData(ins.Config.Entrypoint, ins.Config.Cmd) {
		return nil, fmt.Errorf("启动参数里没有数据目录，已中止")
	}
	if len(ins.HostConfig.PortBindings) == 0 {
		return nil, fmt.Errorf("克隆配置里没有端口，已中止")
	}
	host := map[string]any{
		"Binds":           binds,
		"PortBindings":    ins.HostConfig.PortBindings,
		"RestartPolicy":   ins.HostConfig.RestartPolicy,
		"NetworkMode":     networkMode(ins),
		"Privileged":      ins.HostConfig.Privileged,
		"PublishAllPorts": false,
		"AutoRemove":      false,
		"ReadonlyRootfs":  ins.HostConfig.ReadonlyRootfs,
	}
	if len(ins.HostConfig.ExtraHosts) > 0 {
		host["ExtraHosts"] = ins.HostConfig.ExtraHosts
	}
	if len(ins.HostConfig.DNS) > 0 {
		host["Dns"] = ins.HostConfig.DNS
	}
	if len(ins.HostConfig.DNSSearch) > 0 {
		host["DnsSearch"] = ins.HostConfig.DNSSearch
	}
	body := map[string]any{
		"Image":      image,
		"HostConfig": host,
	}
	if len(ins.Config.Entrypoint) > 0 {
		body["Entrypoint"] = ins.Config.Entrypoint
	}
	if len(ins.Config.Cmd) > 0 {
		body["Cmd"] = ins.Config.Cmd
	}
	if len(ins.Config.Env) > 0 {
		body["Env"] = ins.Config.Env
	}
	if len(ins.Config.Labels) > 0 {
		body["Labels"] = ins.Config.Labels
	}
	if len(ins.Config.ExposedPorts) > 0 {
		body["ExposedPorts"] = ins.Config.ExposedPorts
	}
	if ins.Config.User != "" {
		body["User"] = ins.Config.User
	}
	if ins.Config.WorkingDir != "" {
		body["WorkingDir"] = ins.Config.WorkingDir
	}
	if ins.Config.StopSignal != "" {
		body["StopSignal"] = ins.Config.StopSignal
	}
	if hostName := strings.TrimSpace(ins.Config.Hostname); hostName != "" && !containerIDRe.MatchString(hostName) {
		body["Hostname"] = hostName
	}
	return body, nil
}

func bindsFromMounts(mounts []dockerMount) []string {
	var binds []string
	for _, m := range mounts {
		if m.Type != "" && m.Type != "bind" {
			continue
		}
		if m.Source == "" || m.Destination == "" {
			continue
		}
		b := m.Source + ":" + m.Destination
		if !m.RW {
			b += ":ro"
		}
		binds = append(binds, b)
	}
	return binds
}

func bindsKeepData(binds []string) bool {
	for _, b := range binds {
		dest := bindDest(b)
		if dest == "/var/lib/liking" || strings.HasPrefix(dest, "/var/lib/liking/") {
			return true
		}
	}
	return false
}

func bindDest(bind string) string {
	parts := strings.Split(bind, ":")
	if len(parts) < 2 {
		return ""
	}
	last := parts[len(parts)-1]
	if len(parts) >= 3 && (last == "ro" || last == "rw" || strings.Contains(last, "z") || strings.Contains(last, "Z")) {
		return parts[len(parts)-2]
	}
	return parts[1]
}

func commandKeepsData(entrypoint, cmd []string) bool {
	joined := strings.Join(append(append([]string{}, entrypoint...), cmd...), " ")
	return strings.Contains(joined, "/var/lib/liking/")
}

func networkMode(ins dockerInspect) string {
	mode := strings.TrimSpace(ins.HostConfig.NetworkMode)
	if mode != "" && mode != "default" {
		return mode
	}
	for name := range ins.NetworkSettings.Networks {
		if name != "" && name != "bridge" && name != "host" && name != "none" {
			return name
		}
	}
	if mode == "" {
		return "bridge"
	}
	return mode
}

func recreateImage(ins dockerInspect) string {
	img := strings.TrimSpace(ins.Config.Image)
	if img != "" && !strings.HasPrefix(img, "sha256:") && !strings.ContainsAny(img, " \t") {
		return img
	}
	return "liking:panel"
}

func panelImageTags(ins dockerInspect) []string {
	tags := []string{"liking:panel"}
	img := strings.TrimSpace(ins.Config.Image)
	if img != "" && !strings.HasPrefix(img, "sha256:") && img != "liking:panel" && !strings.ContainsAny(img, " \t") {
		tags = append(tags, img)
	}
	return tags
}

func panelBinary(ins dockerInspect) string {
	for _, c := range append(append([]string{}, ins.Config.Entrypoint...), ins.Config.Cmd...) {
		if c == "/usr/local/bin/liking-server" || strings.HasSuffix(c, "/liking-server") {
			return c
		}
	}
	return "/usr/local/bin/liking-server"
}

func containerName(name string) string {
	return strings.TrimPrefix(strings.TrimSpace(name), "/")
}

func validContainerRef(s string) bool {
	return containerIDRe.MatchString(s) || containerNameRe.MatchString(s)
}

func sameContainer(a, b string) bool {
	a = strings.ToLower(strings.TrimPrefix(a, "/"))
	b = strings.ToLower(strings.TrimPrefix(b, "/"))
	if len(a) < 12 || len(b) < 12 {
		return false
	}
	return a == b || strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "/")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func proxyEnviron() []string {
	var out []string
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy", "LIKING_GH_PROXY"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			out = append(out, k+"="+v)
		}
	}
	if p := strings.TrimSpace(readGHProxy()); p != "" && os.Getenv("LIKING_GH_PROXY") == "" {
		out = append(out, "LIKING_GH_PROXY="+p)
	}
	return out
}

func resolveUpgradeTag(release string) (string, error) {
	release = strings.TrimSpace(release)
	if release != "" {
		if !releaseTagRe.MatchString(release) {
			return "", fmt.Errorf("版本号无效")
		}
		return release, nil
	}
	tag, err := fetchPanelLatest()
	if err == nil {
		return tag, nil
	}
	if strings.TrimSpace(readGHProxy()) == "" {
		t := os.Getenv("LIKING_GH_PROXY")
		_ = os.Setenv("LIKING_GH_PROXY", fallbackGHProxy)
		tag, err2 := fetchPanelLatest()
		if t == "" {
			_ = os.Unsetenv("LIKING_GH_PROXY")
		} else {
			_ = os.Setenv("LIKING_GH_PROXY", t)
		}
		if err2 == nil {
			return tag, nil
		}
	}
	return "", fmt.Errorf("查不到最新版本")
}

func stageRelease(ctx context.Context, tag, dir, goarch string, log *upgradeLogger) error {
	if goarch != "amd64" && goarch != "arm64" {
		return fmt.Errorf("不支持的架构")
	}
	bases, err := downloadBases(tag)
	if err != nil {
		return err
	}
	sums, from, err := fetchSums(ctx, bases)
	if err != nil {
		return err
	}
	log.printf("已核对 SHA256SUMS（%s）", from)
	tmp, err := os.MkdirTemp(dir, ".upgrade-tmp-")
	if err != nil {
		return fmt.Errorf("建临时目录失败")
	}
	defer os.RemoveAll(tmp)

	serverSrc, serverSum, err := downloadFirst(ctx, bases, serverAssetNames(goarch), sums, tmp)
	if err != nil {
		return err
	}
	if err := installVerified(serverSrc, filepath.Join(dir, "liking-server"), serverSum); err != nil {
		return err
	}
	agentSrc, agentSum, err := downloadFirst(ctx, bases, agentAssetNames(goarch), sums, tmp)
	if err != nil {
		return err
	}
	if err := installVerified(agentSrc, filepath.Join(dir, "liking-agent-linux-"+goarch), agentSum); err != nil {
		return err
	}
	if other := otherGoarch(goarch); other != "" {
		otherSrc, otherSum, err := downloadFirst(ctx, bases, []string{"liking-agent-linux-" + other}, sums, tmp)
		if err != nil {
			log.printf("没下到 liking-agent-linux-%s，这个架构的节点暂时不能一键升级", other)
		} else if err := installVerified(otherSrc, filepath.Join(dir, "liking-agent-linux-"+other), otherSum); err != nil {
			log.printf("没放进 liking-agent-linux-%s：%s", other, err.Error())
		}
	}
	return nil
}

func serverAssetNames(goarch string) []string {
	names := []string{"liking-server-linux-" + goarch}
	if goarch == "amd64" {
		names = append(names, "liking-server")
	}
	return names
}

func agentAssetNames(goarch string) []string {
	names := []string{"liking-agent-linux-" + goarch}
	if goarch == "amd64" {
		names = append(names, "liking-agent")
	}
	return names
}

func otherGoarch(goarch string) string {
	switch goarch {
	case "amd64":
		return "arm64"
	case "arm64":
		return "amd64"
	default:
		return ""
	}
}

func officialAssetBase(tag string) (string, error) {
	if !releaseTagRe.MatchString(tag) {
		return "", fmt.Errorf("版本号无效")
	}
	return "https://github.com/" + githubReleaseRepo + "/releases/download/" + tag, nil
}

func downloadBases(tag string) ([]string, error) {
	official, err := officialAssetBase(tag)
	if err != nil {
		return nil, err
	}
	out := []string{official}
	seen := map[string]bool{official: true}
	add := func(prefix string) {
		joined, err := withProxy(prefix, official)
		if err != nil || joined == "" || seen[joined] {
			return
		}
		seen[joined] = true
		out = append(out, joined)
	}
	add(readGHProxy())
	add(fallbackGHProxy)
	return out, nil
}

func withProxy(prefix, raw string) (string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "", nil
	}
	u, err := url.Parse(prefix)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("代理地址无效")
	}
	return strings.TrimRight(u.String(), "/") + "/" + raw, nil
}

func releaseAssetURL(base, asset string) (string, error) {
	if !assetNameRe.MatchString(asset) {
		return "", fmt.Errorf("文件名无效")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("下载地址无效")
	}
	if !allowDownloadHost(u.Hostname()) {
		return "", fmt.Errorf("下载地址无效")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + asset
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func allowDownloadHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	switch host {
	case "github.com", "gh-proxy.com", "www.gh-proxy.com":
		return true
	}
	if strings.HasSuffix(host, ".githubusercontent.com") {
		return true
	}
	if p := strings.TrimSpace(readGHProxy()); p != "" {
		if u, err := url.Parse(p); err == nil && strings.EqualFold(u.Hostname(), host) {
			return true
		}
	}
	return false
}

func fetchSums(ctx context.Context, bases []string) ([]byte, string, error) {
	var last error
	for _, base := range bases {
		raw, err := releaseAssetURL(base, "SHA256SUMS")
		if err != nil {
			last = err
			continue
		}
		b, err := httpGetLimited(ctx, raw, maxSumsBytes, 20*time.Second)
		if err != nil {
			last = err
			continue
		}
		if !looksLikeSums(b) {
			last = fmt.Errorf("SHA256SUMS 内容无效")
			continue
		}
		u, _ := url.Parse(raw)
		host := ""
		if u != nil {
			host = u.Hostname()
		}
		return b, host, nil
	}
	if last == nil {
		last = fmt.Errorf("未取到")
	}
	return nil, "", fmt.Errorf("未取到 SHA256SUMS：%s", last.Error())
}

func looksLikeSums(b []byte) bool {
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && len(fields[0]) == 64 && isHex(fields[0]) && assetNameRe.MatchString(strings.TrimPrefix(fields[1], "*")) {
			return true
		}
	}
	return false
}

func downloadFirst(ctx context.Context, bases, assets []string, sums []byte, dir string) (string, string, error) {
	var last error
	for _, asset := range assets {
		want, err := expectedSHA256(sums, asset)
		if err != nil {
			last = err
			continue
		}
		dest := filepath.Join(dir, asset)
		for _, base := range bases {
			raw, err := releaseAssetURL(base, asset)
			if err != nil {
				return "", "", err
			}
			if err := downloadToFile(ctx, raw, dest, maxReleaseBytes, 90*time.Second); err != nil {
				last = err
				os.Remove(dest)
				continue
			}
			if err := checkReleaseBinary(dest); err != nil {
				last = err
				os.Remove(dest)
				continue
			}
			if err := sha256Equal(want, dest); err != nil {
				last = err
				os.Remove(dest)
				continue
			}
			return dest, want, nil
		}
	}
	if last == nil {
		last = fmt.Errorf("下载失败")
	}
	return "", "", fmt.Errorf("下载失败：%s", last.Error())
}

func expectedSHA256(sums []byte, asset string) (string, error) {
	if !assetNameRe.MatchString(asset) {
		return "", fmt.Errorf("文件名无效")
	}
	for _, line := range strings.Split(string(sums), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || len(fields[0]) != 64 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == asset && isHex(fields[0]) {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("SHA256SUMS 中没有 %s", asset)
}

func isHex(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

func checkReleaseBinary(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.Size() <= minReleaseBytes || st.Size() > maxReleaseBytes {
		return fmt.Errorf("文件大小异常")
	}
	if !fileIsELF(path) {
		return fmt.Errorf("不是 ELF")
	}
	return nil
}

func fileIsELF(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var mag [4]byte
	if _, err := io.ReadFull(f, mag[:]); err != nil {
		return false
	}
	return mag == [4]byte{0x7f, 'E', 'L', 'F'}
}

func sha256Equal(sumHex, path string) error {
	want, err := hex.DecodeString(sumHex)
	if err != nil || len(want) != sha256.Size {
		return fmt.Errorf("sha256 无效")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, maxReleaseBytes+1)); err != nil {
		return err
	}
	got := h.Sum(nil)
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return fmt.Errorf("sha256 校验失败")
	}
	return nil
}

func installVerified(src, dst, wantHex string) error {
	if err := checkReleaseBinary(src); err != nil {
		return err
	}
	if err := sha256Equal(wantHex, src); err != nil {
		return err
	}
	if sameFile(src, dst) {
		return os.Chmod(dst, 0o755)
	}
	if err := copyFileMode(src, dst, 0o755); err != nil {
		return err
	}
	if err := sha256Equal(wantHex, dst); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}

func sameFile(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ia, ib)
}

func copyFileMode(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, io.LimitReader(in, maxReleaseBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func dockerfileReady(dir string) error {
	b, err := os.ReadFile(filepath.Join(dir, "Dockerfile"))
	if err != nil {
		return fmt.Errorf("没有 Dockerfile，正在运行的容器没有被替换")
	}
	text := string(b)
	for _, name := range []string{"liking-server", "ca-certificates.crt"} {
		if strings.Contains(text, name) {
			st, err := os.Stat(filepath.Join(dir, name))
			if err != nil || st.IsDir() {
				return fmt.Errorf("构建目录缺少 %s，正在运行的容器没有被替换", name)
			}
		}
	}
	if !fileIsELF(filepath.Join(dir, "liking-server")) {
		return fmt.Errorf("liking-server 不是 ELF，正在运行的容器没有被替换")
	}
	return nil
}

func httpGetLimited(ctx context.Context, rawURL string, max int64, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "liking-panel")
	res, err := releaseHTTP().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("文件过大")
	}
	return b, nil
}

func downloadToFile(ctx context.Context, rawURL, dest string, max int64, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "liking-panel")
	res, err := releaseHTTP().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	tmp := dest + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(f, io.LimitReader(res.Body, max+1))
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil || n > max {
		_ = os.Remove(tmp)
		if n > max {
			return fmt.Errorf("文件过大")
		}
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	return os.Rename(tmp, dest)
}

func releaseHTTP() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return fmt.Errorf("重定向过多")
			}
			if req.URL.Scheme != "https" || !allowDownloadHost(req.URL.Hostname()) {
				return fmt.Errorf("下载地址无效")
			}
			return nil
		},
	}
}

type upgradeLogger struct {
	f *os.File
}

func openUpgradeLog(dir string) *upgradeLogger {
	f, err := os.OpenFile(filepath.Join(dir, "upgrade.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return &upgradeLogger{}
	}
	return &upgradeLogger{f: f}
}

func (l *upgradeLogger) printf(format string, args ...any) {
	if l == nil {
		return
	}
	line := time.Now().Format("2006-01-02 15:04:05 ") + fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, line)
	if l.f != nil {
		fmt.Fprintln(l.f, line)
	}
}

func (l *upgradeLogger) close() {
	if l != nil && l.f != nil {
		_ = l.f.Close()
	}
}

func appendUpgradeLog(dir, format string, args ...any) {
	log := openUpgradeLog(dir)
	log.printf(format, args...)
	log.close()
}

type dockerAPI struct {
	base string
	do   func(req *http.Request) (*http.Response, error)
}

type dockerStatusError struct {
	Status  int
	Message string
}

func (e *dockerStatusError) Error() string { return e.Message }

func isNotFound(err error) bool {
	var se *dockerStatusError
	return errors.As(err, &se) && se.Status == http.StatusNotFound
}

func dialDocker(sock string) (*dockerAPI, error) {
	st, err := os.Stat(sock)
	if err != nil || st.IsDir() {
		return nil, fmt.Errorf("没有 docker.sock")
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		},
	}
	client := &http.Client{Transport: transport}
	return &dockerAPI{base: "http://docker", do: client.Do}, nil
}

func (d *dockerAPI) call(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, d.base+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := d.do(req)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (d *dockerAPI) inspect(ctx context.Context, ref string) (dockerInspect, error) {
	var ins dockerInspect
	if !validContainerRef(ref) {
		return ins, fmt.Errorf("容器编号无效")
	}
	res, err := d.call(ctx, http.MethodGet, dockerAPIPrefix+"/containers/"+url.PathEscape(ref)+"/json", nil, "")
	if err != nil {
		return ins, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ins, dockerStatus(res)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&ins); err != nil {
		return ins, err
	}
	return ins, nil
}

func (d *dockerAPI) list(ctx context.Context) ([]dockerListItem, error) {
	res, err := d.call(ctx, http.MethodGet, dockerAPIPrefix+"/containers/json", nil, "")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, dockerStatus(res)
	}
	var items []dockerListItem
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&items); err != nil {
		return nil, err
	}
	return items, nil
}

func (d *dockerAPI) running(ctx context.Context, ref string) (bool, error) {
	ins, err := d.inspect(ctx, ref)
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return ins.State.Running, nil
}

func (d *dockerAPI) create(ctx context.Context, name string, body any) (string, error) {
	if !containerNameRe.MatchString(name) {
		return "", fmt.Errorf("容器名称无效")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	res, err := d.call(ctx, http.MethodPost, dockerAPIPrefix+"/containers/create?name="+url.QueryEscape(name), strings.NewReader(string(raw)), "application/json")
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		return "", dockerStatus(res)
	}
	var out struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("创建容器没有返回编号")
	}
	return out.ID, nil
}

func (d *dockerAPI) start(ctx context.Context, ref string) error {
	if !validContainerRef(ref) {
		return fmt.Errorf("容器编号无效")
	}
	res, err := d.call(ctx, http.MethodPost, dockerAPIPrefix+"/containers/"+url.PathEscape(ref)+"/start", nil, "")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotModified || res.StatusCode == http.StatusNoContent || res.StatusCode == http.StatusOK {
		return nil
	}
	return dockerStatus(res)
}

func (d *dockerAPI) stop(ctx context.Context, ref string, timeoutSec int) error {
	if !validContainerRef(ref) {
		return fmt.Errorf("容器编号无效")
	}
	if timeoutSec < 1 || timeoutSec > 120 {
		timeoutSec = 20
	}
	path := fmt.Sprintf("%s/containers/%s/stop?t=%d", dockerAPIPrefix, url.PathEscape(ref), timeoutSec)
	res, err := d.call(ctx, http.MethodPost, path, nil, "")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotModified || res.StatusCode == http.StatusNoContent || res.StatusCode == http.StatusOK {
		return nil
	}
	return dockerStatus(res)
}

func (d *dockerAPI) rename(ctx context.Context, ref, name string) error {
	if !validContainerRef(ref) || !containerNameRe.MatchString(name) {
		return fmt.Errorf("容器名称无效")
	}
	path := dockerAPIPrefix + "/containers/" + url.PathEscape(ref) + "/rename?name=" + url.QueryEscape(name)
	res, err := d.call(ctx, http.MethodPost, path, nil, "")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNoContent || res.StatusCode == http.StatusOK {
		return nil
	}
	return dockerStatus(res)
}

func (d *dockerAPI) removeForce(ctx context.Context, ref string) error {
	if !validContainerRef(ref) {
		return fmt.Errorf("容器编号无效")
	}
	path := dockerAPIPrefix + "/containers/" + url.PathEscape(ref) + "?force=1&v=0"
	res, err := d.call(ctx, http.MethodDelete, path, nil, "")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNoContent || res.StatusCode == http.StatusOK || res.StatusCode == http.StatusNotFound {
		return nil
	}
	return dockerStatus(res)
}

func (d *dockerAPI) removeIfStopped(ctx context.Context, ref string) error {
	ins, err := d.inspect(ctx, ref)
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	if ins.State.Running {
		return fmt.Errorf("%s 正在运行", ref)
	}
	return d.removeForce(ctx, ref)
}

func (d *dockerAPI) buildImage(ctx context.Context, dir string, tags []string, log *upgradeLogger) error {
	if len(tags) == 0 {
		return fmt.Errorf("没有镜像名")
	}
	q := url.Values{}
	for _, tag := range tags {
		if tag == "" || strings.ContainsAny(tag, " \t\r\n") {
			return fmt.Errorf("镜像名无效")
		}
		q.Add("t", tag)
	}
	q.Set("dockerfile", "Dockerfile")
	q.Set("rm", "1")
	q.Set("forcerm", "1")
	pr, pw := io.Pipe()
	go func() {
		err := writeDockerContext(dir, pw)
		_ = pw.CloseWithError(err)
	}()
	res, err := d.call(ctx, http.MethodPost, dockerAPIPrefix+"/build?"+q.Encode(), pr, "application/x-tar")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return dockerStatus(res)
	}
	dec := json.NewDecoder(io.LimitReader(res.Body, 8<<20))
	for {
		var line struct {
			Stream string `json:"stream"`
			Error  string `json:"error"`
		}
		if err := dec.Decode(&line); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if line.Error != "" {
			return fmt.Errorf("%s", line.Error)
		}
		stream := strings.TrimSpace(line.Stream)
		if stream != "" && (strings.HasPrefix(stream, "Step") || strings.Contains(stream, "Successfully")) {
			log.printf("%s", stream)
		}
	}
}

func writeDockerContext(dir string, w io.Writer) error {
	tw := tar.NewWriter(w)
	entries, err := os.ReadDir(dir)
	if err != nil {
		_ = tw.Close()
		return err
	}
	for _, ent := range entries {
		name := ent.Name()
		if skipContextName(name) || ent.IsDir() {
			continue
		}
		info, err := ent.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxReleaseBytes {
			continue
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			_ = tw.Close()
			return err
		}
		hdr.Name = name
		if err := tw.WriteHeader(hdr); err != nil {
			_ = tw.Close()
			return err
		}
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			_ = tw.Close()
			return err
		}
		_, copyErr := io.Copy(tw, f)
		_ = f.Close()
		if copyErr != nil {
			_ = tw.Close()
			return copyErr
		}
	}
	return tw.Close()
}

func skipContextName(name string) bool {
	if name == "upgrade.log" || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".part") {
		return true
	}
	return false
}

func dockerStatus(res *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
	var msg struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(b, &msg)
	if msg.Message == "" {
		msg.Message = strings.TrimSpace(string(b))
	}
	if msg.Message == "" {
		msg.Message = res.Status
	}
	return &dockerStatusError{Status: res.StatusCode, Message: msg.Message}
}

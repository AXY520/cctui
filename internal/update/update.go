package update

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const userAgent = "cctui-updater"

const (
	giteeAPILatest   = "https://gitee.com/api/v5/repos/aixinyin/cctui/releases/latest"
	giteeReleasesWeb = "https://gitee.com/aixinyin/cctui/releases"
	giteeDownloadFmt = "https://gitee.com/aixinyin/cctui/releases/download/%s/%s"
	githubAPILatest  = "https://api.github.com/repos/AXY520/cctui/releases/latest"
)

var giteeTagRe = regexp.MustCompile(`/aixinyin/cctui/releases/tag/(v?[0-9][^"'?\s#]*)`)

// Info 描述一次可用更新。
type Info struct {
	Current    string
	Latest     string
	Tag        string
	Notes      string
	AssetURL   string
	AssetName  string
	Source     string
	Executable string
}

type releaseResponse struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

type releaseCandidate struct {
	Tag       string
	Latest    string
	Notes     string
	AssetURL  string
	AssetName string
	Source    string
}

// Normalize 去掉 v 前缀并裁剪空白。
func Normalize(version string) string {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	version = strings.TrimPrefix(version, "V")
	return version
}

// Compare 比较两个版本号。返回 -1/0/1。
// 无法解析的版本（如 dev）视为低于任意正式版本。
func Compare(a, b string) int {
	ap := parseVersion(Normalize(a))
	bp := parseVersion(Normalize(b))
	for i := 0; i < 3; i++ {
		if ap[i] < bp[i] {
			return -1
		}
		if ap[i] > bp[i] {
			return 1
		}
	}
	return 0
}

// HasUpdate 判断 latest 是否比 current 新。
func HasUpdate(current, latest string) bool {
	current = Normalize(current)
	latest = Normalize(latest)
	if latest == "" {
		return false
	}
	if current == "" || current == "dev" {
		return true
	}
	return Compare(current, latest) < 0
}

// Check 查询远端最新版本；无更新时返回 (nil, nil)。
//
// 会聚合多个来源，取最高版本，避免：
// 1. Gitee API 限流后误信 GitHub 旧 release；
// 2. 某一源返回“无更新”就提前结束，漏掉其他源的新版本。
func Check(current string) (*Info, error) {
	client := &http.Client{Timeout: 12 * time.Second}
	wanted := assetName()

	var (
		candidates []releaseCandidate
		errs       []string
	)

	addErr := func(err error) {
		if err == nil {
			return
		}
		msg := strings.TrimSpace(err.Error())
		if msg == "" {
			return
		}
		for _, existing := range errs {
			if existing == msg {
				return
			}
		}
		errs = append(errs, msg)
	}

	if c, err := fetchGiteeAPI(client, wanted); err != nil {
		addErr(err)
	} else if c != nil {
		candidates = append(candidates, *c)
	}

	// HTML 不走 API 配额，限流时仍可用。
	if c, err := fetchGiteeHTML(client, wanted); err != nil {
		addErr(err)
	} else if c != nil {
		candidates = append(candidates, *c)
	}

	if c, err := fetchGitHubAPI(client, wanted); err != nil {
		addErr(err)
	} else if c != nil {
		candidates = append(candidates, *c)
	}

	if len(candidates) == 0 {
		if len(errs) == 0 {
			return nil, fmt.Errorf("未找到可用发布源")
		}
		return nil, fmt.Errorf("检查更新失败: %s", strings.Join(errs, " | "))
	}

	best := candidates[0]
	for _, item := range candidates[1:] {
		if Compare(item.Latest, best.Latest) > 0 {
			best = item
		}
	}

	if !HasUpdate(current, best.Latest) {
		return nil, nil
	}

	exe, _ := CurrentExecutable()
	return &Info{
		Current:    Normalize(current),
		Latest:     best.Latest,
		Tag:        best.Tag,
		Notes:      best.Notes,
		AssetURL:   best.AssetURL,
		AssetName:  best.AssetName,
		Source:     best.Source,
		Executable: exe,
	}, nil
}

// Apply 下载并替换当前可执行文件，返回安装路径。
func Apply(info *Info) (string, error) {
	if info == nil {
		return "", fmt.Errorf("更新信息为空")
	}
	if strings.TrimSpace(info.AssetURL) == "" {
		return "", fmt.Errorf("缺少下载地址")
	}

	exe := strings.TrimSpace(info.Executable)
	if exe == "" {
		var err error
		exe, err = os.Executable()
		if err != nil {
			return "", fmt.Errorf("定位当前程序失败: %w", err)
		}
	}
	exe, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("解析程序路径失败: %w", err)
	}

	if err := ensureWritable(exe); err != nil {
		return "", err
	}

	tmpDir, err := os.MkdirTemp("", "cctui-update-*")
	if err != nil {
		return "", fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	asset := strings.TrimSpace(info.AssetName)
	if asset == "" {
		asset = "release.tar.gz"
	}
	archivePath := filepath.Join(tmpDir, asset)

	client := &http.Client{Timeout: 90 * time.Second}
	if err := downloadFile(client, info.AssetURL, archivePath); err != nil {
		return "", err
	}

	binaryPath, err := extractBinary(archivePath, tmpDir)
	if err != nil {
		return "", err
	}

	if err := os.Chmod(binaryPath, 0o755); err != nil {
		return "", fmt.Errorf("设置可执行权限失败: %w", err)
	}

	if err := replaceExecutable(exe, binaryPath); err != nil {
		return "", err
	}

	return exe, nil
}

// CurrentExecutable 返回当前二进制真实路径。
func CurrentExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func fetchGiteeAPI(client *http.Client, wanted string) (*releaseCandidate, error) {
	endpoint := giteeAPILatest
	if token := giteeToken(); token != "" {
		endpoint += "?access_token=" + url.QueryEscape(token)
	}
	release, err := fetchReleaseJSON(client, endpoint, "gitee-api")
	if err != nil {
		return nil, err
	}
	return candidateFromRelease(release, wanted, "gitee-api", func(tag, name string) string {
		return fmt.Sprintf(giteeDownloadFmt, tag, name)
	})
}

func fetchGitHubAPI(client *http.Client, wanted string) (*releaseCandidate, error) {
	release, err := fetchReleaseJSON(client, githubAPILatest, "github")
	if err != nil {
		return nil, err
	}
	return candidateFromRelease(release, wanted, "github", nil)
}

func fetchGiteeHTML(client *http.Client, wanted string) (*releaseCandidate, error) {
	req, err := http.NewRequest(http.MethodGet, giteeReleasesWeb, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; cctui-updater)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitee-html: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("gitee-html: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("gitee-html: 读取失败: %w", err)
	}

	matches := giteeTagRe.FindAllStringSubmatch(string(body), -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("gitee-html: 未解析到 release tag")
	}

	bestTag := ""
	bestLatest := ""
	seen := map[string]bool{}
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		tag := strings.TrimSpace(match[1])
		tag = strings.TrimSuffix(tag, "/")
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		latest := Normalize(tag)
		if latest == "" {
			continue
		}
		if bestLatest == "" || Compare(latest, bestLatest) > 0 {
			bestTag = tag
			bestLatest = latest
		}
	}
	if bestTag == "" {
		return nil, fmt.Errorf("gitee-html: 未找到有效版本")
	}
	if !strings.HasPrefix(bestTag, "v") && !strings.HasPrefix(bestTag, "V") {
		bestTag = "v" + bestTag
	}

	assetURL := fmt.Sprintf(giteeDownloadFmt, bestTag, wanted)
	// 用 HEAD/GET 探活；有的环境 HEAD 被拒，失败不直接判死，仍返回 URL。
	if err := probeAssetURL(client, assetURL); err != nil {
		// 仍然返回候选，真正下载时再报错；但给 source 标记
		return &releaseCandidate{
			Tag:       bestTag,
			Latest:    bestLatest,
			AssetURL:  assetURL,
			AssetName: wanted,
			Source:    "gitee-html",
		}, nil
	}

	return &releaseCandidate{
		Tag:       bestTag,
		Latest:    bestLatest,
		AssetURL:  assetURL,
		AssetName: wanted,
		Source:    "gitee-html",
	}, nil
}

func fetchReleaseJSON(client *http.Client, endpoint, source string) (*releaseResponse, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("%s: 读取失败: %w", source, err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		// 限流文案更直白
		if resp.StatusCode == http.StatusForbidden && strings.Contains(strings.ToLower(msg), "rate limit") {
			return nil, fmt.Errorf("%s: 接口限流 (HTTP 403)", source)
		}
		return nil, fmt.Errorf("%s: HTTP %d %s", source, resp.StatusCode, msg)
	}

	var release releaseResponse
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("%s: 解析发布信息失败: %w", source, err)
	}
	return &release, nil
}

func candidateFromRelease(release *releaseResponse, wanted, source string, fallbackURL func(tag, name string) string) (*releaseCandidate, error) {
	if release == nil {
		return nil, fmt.Errorf("%s: 发布信息为空", source)
	}
	tag := strings.TrimSpace(release.TagName)
	if tag == "" {
		tag = strings.TrimSpace(release.Name)
	}
	latest := Normalize(tag)
	if latest == "" {
		return nil, fmt.Errorf("%s: 发布版本为空", source)
	}
	if !strings.HasPrefix(tag, "v") && !strings.HasPrefix(tag, "V") {
		tag = "v" + latest
	}

	assetURL := ""
	assetNameValue := wanted
	for _, asset := range release.Assets {
		if asset.Name != wanted {
			continue
		}
		assetNameValue = asset.Name
		if strings.TrimSpace(asset.BrowserDownloadURL) != "" {
			assetURL = strings.TrimSpace(asset.BrowserDownloadURL)
			break
		}
	}
	if assetURL == "" && fallbackURL != nil {
		assetURL = fallbackURL(tag, wanted)
	}
	if assetURL == "" {
		return nil, fmt.Errorf("%s: 未找到 %s", source, wanted)
	}

	return &releaseCandidate{
		Tag:       tag,
		Latest:    latest,
		Notes:     strings.TrimSpace(release.Body),
		AssetURL:  assetURL,
		AssetName: assetNameValue,
		Source:    source,
	}, nil
}

func probeAssetURL(client *http.Client, assetURL string) error {
	req, err := http.NewRequest(http.MethodHead, assetURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return nil
	}
	// 有的 CDN/Gitee 对 HEAD 不友好，再试一次 GET 但不读完
	req, err = http.NewRequest(http.MethodGet, assetURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err = client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return nil
	}
	return fmt.Errorf("HTTP %d", resp.StatusCode)
}

func giteeToken() string {
	for _, key := range []string{"CCTUI_GITEE_TOKEN", "GITEE_TOKEN"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func assetName() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	switch goarch {
	case "x86_64":
		goarch = "amd64"
	case "aarch64":
		goarch = "arm64"
	}
	return fmt.Sprintf("cctui-%s-%s.tar.gz", goos, goarch)
}

func parseVersion(version string) [3]int {
	var out [3]int
	version = strings.TrimSpace(version)
	if version == "" || version == "dev" {
		return out
	}
	// 只取主版本号部分，忽略 -beta 等后缀
	if idx := strings.IndexAny(version, "-+"); idx >= 0 {
		version = version[:idx]
	}
	parts := strings.Split(version, ".")
	for i := 0; i < len(parts) && i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return [3]int{}
		}
		out[i] = n
	}
	return out
}

func downloadFile(client *http.Client, rawURL, dest string) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	file, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	return nil
}

func extractBinary(archivePath, destDir string) (string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("打开压缩包失败: %w", err)
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return "", fmt.Errorf("解压 gzip 失败: %w", err)
	}
	defer gz.Close()

	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("读取压缩包失败: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}

		name := filepath.Base(header.Name)
		if name != "cctui" && !strings.HasPrefix(name, "cctui-") {
			continue
		}

		target := filepath.Join(destDir, "cctui.new")
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return "", fmt.Errorf("写出二进制失败: %w", err)
		}
		if _, err := io.Copy(out, reader); err != nil {
			out.Close()
			return "", fmt.Errorf("写出二进制失败: %w", err)
		}
		if err := out.Close(); err != nil {
			return "", err
		}
		return target, nil
	}

	return "", fmt.Errorf("压缩包中未找到 cctui 二进制")
}

func ensureWritable(path string) error {
	dir := filepath.Dir(path)
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("检查安装目录失败: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("安装路径无效: %s", dir)
	}

	// 只检查目录可写。Linux 上正在运行的二进制用 O_WRONLY 打开会 ETXTBSY，
	// 自更新应通过同目录写 .new + rename 完成，不能要求“当前文件可写”。
	testFile := filepath.Join(dir, ".cctui-write-test")
	if err := os.WriteFile(testFile, []byte("ok"), 0o600); err != nil {
		return fmt.Errorf("安装目录不可写: %s（可改用 cctui update 到用户目录，或 install.sh）", dir)
	}
	_ = os.Remove(testFile)
	return nil
}

func replaceExecutable(target, source string) error {
	backup := target + ".bak"
	temp := target + ".new"

	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("读取新版本失败: %w", err)
	}

	// 先写临时文件到同目录，再原子 rename。这样即使 target 正在运行也能替换。
	if err := os.WriteFile(temp, data, 0o755); err != nil {
		return fmt.Errorf("写入新版本失败: %w", err)
	}
	_ = os.Chmod(temp, 0o755)

	// 备份旧文件（运行中 rename 在 Linux 通常可行）
	_ = os.Remove(backup)
	if err := os.Rename(target, backup); err != nil {
		// 部分系统/权限下 rename 失败：尝试直接用新文件顶上（仍避免 truncation 写运行中文件）
		if err2 := os.Rename(temp, target); err2 != nil {
			_ = os.Remove(temp)
			return fmt.Errorf("替换失败: 无法备份旧版本(%v)，也无法启用新版本(%v)。若安装在系统目录，请用有权限的方式安装到 ~/.local/bin", err, err2)
		}
		return nil
	}

	if err := os.Rename(temp, target); err != nil {
		// 回滚
		_ = os.Rename(backup, target)
		_ = os.Remove(temp)
		return fmt.Errorf("启用新版本失败: %w", err)
	}

	_ = os.Chmod(target, 0o755)
	return nil
}

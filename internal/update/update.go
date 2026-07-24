package update

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const userAgent = "cctui-updater"

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

type releaseSource struct {
	Name string
	URL  string
}

var defaultSources = []releaseSource{
	{Name: "gitee", URL: "https://gitee.com/api/v5/repos/aixinyin/cctui/releases/latest"},
	{Name: "github", URL: "https://api.github.com/repos/AXY520/cctui/releases/latest"},
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
func Check(current string) (*Info, error) {
	client := &http.Client{Timeout: 8 * time.Second}
	wanted := assetName()
	var lastErr error

	for _, source := range defaultSources {
		info, err := checkSource(client, source, current, wanted)
		if err != nil {
			lastErr = err
			continue
		}
		return info, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("未找到可用发布源")
	}
	return nil, lastErr
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

func checkSource(client *http.Client, source releaseSource, current, wanted string) (*Info, error) {
	req, err := http.NewRequest(http.MethodGet, source.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("%s: HTTP %d %s", source.Name, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var release releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("%s: 解析发布信息失败: %w", source.Name, err)
	}

	tag := strings.TrimSpace(release.TagName)
	if tag == "" {
		tag = strings.TrimSpace(release.Name)
	}
	latest := Normalize(tag)
	if latest == "" {
		return nil, fmt.Errorf("%s: 发布版本为空", source.Name)
	}

	assetURL := ""
	assetNameValue := wanted
	for _, asset := range release.Assets {
		if asset.Name == wanted && strings.TrimSpace(asset.BrowserDownloadURL) != "" {
			assetURL = asset.BrowserDownloadURL
			assetNameValue = asset.Name
			break
		}
	}
	if assetURL == "" {
		return nil, fmt.Errorf("%s: 未找到 %s", source.Name, wanted)
	}

	if !HasUpdate(current, latest) {
		return nil, nil
	}

	exe, _ := CurrentExecutable()
	return &Info{
		Current:    Normalize(current),
		Latest:     latest,
		Tag:        tag,
		Notes:      strings.TrimSpace(release.Body),
		AssetURL:   assetURL,
		AssetName:  assetNameValue,
		Source:     source.Name,
		Executable: exe,
	}, nil
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

func downloadFile(client *http.Client, url, dest string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
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

	// 尝试在同目录创建临时文件判断写权限
	testFile := filepath.Join(dir, ".cctui-write-test")
	if err := os.WriteFile(testFile, []byte("ok"), 0o600); err != nil {
		return fmt.Errorf("安装目录不可写: %s（可改用 install.sh 或手动替换）", dir)
	}
	_ = os.Remove(testFile)

	// 目标文件存在时也要可写
	if _, err := os.Stat(path); err == nil {
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			return fmt.Errorf("当前程序不可写: %s", path)
		}
		file.Close()
	}
	return nil
}

func replaceExecutable(target, source string) error {
	backup := target + ".bak"
	temp := target + ".new"

	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("读取新版本失败: %w", err)
	}
	if err := os.WriteFile(temp, data, 0o755); err != nil {
		return fmt.Errorf("写入新版本失败: %w", err)
	}

	// 备份旧文件；失败不阻断更新
	_ = os.Remove(backup)
	if err := os.Rename(target, backup); err != nil {
		// 某些环境 rename 运行中文件会失败，尝试直接覆盖
		if copyErr := copyFile(source, target); copyErr != nil {
			_ = os.Remove(temp)
			return fmt.Errorf("替换失败: %v / %v", err, copyErr)
		}
		_ = os.Remove(temp)
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

func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

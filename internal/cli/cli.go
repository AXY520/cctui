package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cctui/internal/update"
)

// Run 处理子命令。返回 true 表示已处理并应退出 main。
func Run(version string, args []string) bool {
	if len(args) == 0 {
		return false
	}

	cmd := args[0]
	rest := args[1:]
	switch cmd {
	case "-h", "--help", "help":
		printHelp(version)
		return true
	case "-v", "--version", "version":
		fmt.Println("cctui", version)
		return true
	case "update", "--update", "self-update":
		if err := runUpdate(version, rest); err != nil {
			fatal(err)
		}
		return true
	case "check-update", "--check-update":
		if err := runCheckUpdate(version); err != nil {
			fatal(err)
		}
		return true
	case "uninstall", "--uninstall":
		if err := runUninstall(rest); err != nil {
			fatal(err)
		}
		return true
	default:
		if strings.HasPrefix(cmd, "-") {
			fmt.Fprintf(os.Stderr, "未知参数: %s\n\n", cmd)
			printHelp(version)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", cmd)
		printHelp(version)
		os.Exit(2)
		return true
	}
}

func printHelp(version string) {
	fmt.Printf(`cctui %s — Claude / Codex / Gemini / Pi 供应商切换工具

用法:
  cctui                      启动 TUI
  cctui update [-y]          检查并安装更新
  cctui check-update         仅检查是否有新版本
  cctui uninstall [--purge]  卸载 cctui（--purge 同时删除 ~/.cc-switch）
  cctui version              显示版本
  cctui help                 显示帮助

快捷参数:
  -v, --version
  -h, --help
  -u                         同 update（需注意与 TUI 内快捷键无关）

示例:
  cctui
  cctui update
  cctui update -y
  cctui uninstall --purge
`, version)
}

func runCheckUpdate(version string) error {
	fmt.Printf("当前版本: %s\n", displayVersion(version))
	fmt.Println("正在检查更新...")
	info, err := update.Check(version)
	if err != nil {
		return fmt.Errorf("检查更新失败: %w", err)
	}
	if info == nil {
		fmt.Println("已是最新版本")
		return nil
	}
	fmt.Printf("发现新版本: v%s（来源: %s）\n", info.Latest, info.Source)
	if notes := strings.TrimSpace(info.Notes); notes != "" {
		fmt.Println("更新说明:")
		fmt.Println(indent(notes, "  "))
	}
	fmt.Println("执行 cctui update 进行更新")
	return nil
}

func runUpdate(version string, args []string) error {
	yes := hasFlag(args, "-y", "--yes")
	fmt.Printf("当前版本: %s\n", displayVersion(version))
	fmt.Println("正在检查更新...")
	info, err := update.Check(version)
	if err != nil {
		return fmt.Errorf("检查更新失败: %w", err)
	}
	if info == nil {
		fmt.Println("已是最新版本")
		return nil
	}

	fmt.Printf("发现新版本: v%s（来源: %s）\n", info.Latest, info.Source)
	if notes := strings.TrimSpace(info.Notes); notes != "" {
		fmt.Println("更新说明:")
		fmt.Println(indent(notes, "  "))
	}
	exe := info.Executable
	if exe == "" {
		exe, _ = update.CurrentExecutable()
	}
	if exe != "" {
		fmt.Printf("安装位置: %s\n", exe)
	}

	if !yes {
		ok, err := confirm(fmt.Sprintf("确认更新到 v%s？[Y/n] ", info.Latest), true)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("已取消更新")
			return nil
		}
	}

	fmt.Printf("正在下载并安装 v%s ...\n", info.Latest)
	path, err := update.Apply(info)
	if err != nil {
		return fmt.Errorf("更新失败: %w", err)
	}
	fmt.Printf("已更新到 v%s\n", info.Latest)
	fmt.Printf("路径: %s\n", path)
	fmt.Println("请重新运行 cctui 使用新版本")
	return nil
}

func runUninstall(args []string) error {
	purge := hasFlag(args, "--purge")
	exe, err := update.CurrentExecutable()
	if err != nil {
		return fmt.Errorf("定位当前程序失败: %w", err)
	}

	fmt.Printf("将卸载: %s\n", exe)
	if purge {
		fmt.Printf("并将删除配置目录: %s\n", filepath.Join(homeDir(), ".cc-switch"))
	} else {
		fmt.Printf("配置目录保留: %s\n", filepath.Join(homeDir(), ".cc-switch"))
	}

	ok, err := confirm("确认卸载？[y/N] ", false)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("已取消卸载")
		return nil
	}

	// 先删备份再删本体
	_ = os.Remove(exe + ".bak")
	if err := os.Remove(exe); err != nil {
		return fmt.Errorf("删除程序失败: %w（可能需要手动删除或 sudo）", err)
	}
	fmt.Println("已删除程序文件")

	if purge {
		dir := filepath.Join(homeDir(), ".cc-switch")
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("删除配置目录失败: %w", err)
		}
		fmt.Printf("已删除配置目录: %s\n", dir)
	} else {
		fmt.Printf("如需清理配置: rm -rf %s\n", filepath.Join(homeDir(), ".cc-switch"))
	}
	fmt.Println("卸载完成")
	return nil
}

func confirm(prompt string, defaultYes bool) (bool, error) {
	fmt.Print(prompt)
	// 无 tty 时走默认
	fi, err := os.Stdin.Stat()
	if err != nil || (fi.Mode()&os.ModeCharDevice) == 0 {
		fmt.Println()
		return defaultYes, nil
	}
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && len(strings.TrimSpace(line)) == 0 {
		return defaultYes, nil
	}
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return defaultYes, nil
	}
	switch line {
	case "y", "yes", "是":
		return true, nil
	case "n", "no", "否":
		return false, nil
	default:
		return defaultYes, nil
	}
}

func hasFlag(args []string, flags ...string) bool {
	set := map[string]struct{}{}
	for _, f := range flags {
		set[f] = struct{}{}
	}
	for _, arg := range args {
		if _, ok := set[arg]; ok {
			return true
		}
	}
	return false
}

func displayVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "dev"
	}
	if version == "dev" || strings.HasPrefix(version, "v") || strings.HasPrefix(version, "V") {
		return version
	}
	return "v" + version
}

func indent(text, prefix string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return home
	}
	return "."
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "%v\n", err)
	os.Exit(1)
}

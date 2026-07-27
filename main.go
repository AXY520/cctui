package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"cctui/internal/ccswitch"
	"cctui/internal/cli"
	"cctui/internal/ui"
)

var version = "dev"

func main() {
	ui.Version = version

	// 兼容: cctui -u / cctui --update
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "-u":
			args[0] = "update"
		}
		if cli.Run(version, args) {
			return
		}
	}

	store, err := ccswitch.OpenStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开数据存储失败: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	warnings, err := store.Bootstrap()
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化失败: %v\n", err)
		os.Exit(1)
	}

	model, err := ui.NewModel(store, warnings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化界面失败: %v\n", err)
		os.Exit(1)
	}

	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "程序运行失败: %v\n", err)
		os.Exit(1)
	}
}

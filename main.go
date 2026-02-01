package main

import (
	"ikl/cmd"
	"os"
)

func main() {
	// 将执行权限移交给 Cobra 命令行框架
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

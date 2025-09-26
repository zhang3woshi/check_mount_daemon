package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

const (
	sourceDir = "/vol02/1000-0-122ea9fa/Photos/"
	targetDir = "/vol1/1000/Photos/"
	checkFile = "remote"
	interval  = 30 * time.Second // 检查间隔
)

func main() {
	for {
		checkAndMount()
		time.Sleep(interval)
	}
}

func checkAndMount() {
	// 检查源目录是否存在
	if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
		fmt.Printf("[%s] 源目录 %s 不存在，跳过\n", time.Now().Format(time.RFC3339), sourceDir)
		return
	}

	// 确保目标目录存在
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		fmt.Printf("[%s] 目标目录 %s 不存在，正在创建...\n", time.Now().Format(time.RFC3339), targetDir)
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			fmt.Printf("[%s] 创建目标目录失败: %v\n", time.Now().Format(time.RFC3339), err)
			return
		}
	}

	// 检查目标目录下是否有 remote 文件
	targetFile := targetDir + "/" + checkFile
	if _, err := os.Stat(targetFile); err == nil {
		fmt.Printf("[%s] 挂载正常，无需操作。\n", time.Now().Format(time.RFC3339))
		return
	}

	// 挂载动作
	fmt.Printf("[%s] 未检测到 %s，执行挂载...\n", time.Now().Format(time.RFC3339), targetFile)
	cmd := exec.Command("mount", "--bind", sourceDir, targetDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("[%s] 挂载失败: %v\n输出: %s\n", time.Now().Format(time.RFC3339), err, string(output))
		return
	}
	fmt.Printf("[%s] 挂载成功！\n", time.Now().Format(time.RFC3339))
}

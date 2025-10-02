package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// isRoot 检查当前进程是否以 root 权限运行（仅在类 Unix 系统有效）。
func isRoot() bool {
	// 在 Windows 上暂时不检查
	if runtime.GOOS == "windows" {
		return true
	}
	// 使用 os.Geteuid（需要 go1.12+）
	// 注意：os.Geteuid 在 Windows 平台不可用，但我们之前已短路。
	return os.Geteuid() == 0
}

const (
	targetDir = "/vol1/1000/Photos/"
	checkFile = "remote"
	interval  = 30 * time.Second // 检查间隔
)

// Config 用来保存可配置项
type Config struct {
	SourceDir string `json:"sourceDir"`
}

// loadConfig 从 config.json 加载配置，如果不存在则返回空配置。
// 环境变量 SOURCE_DIR 会覆盖配置文件中的值。
func loadConfig() Config {
	cfg := Config{}

	// 1) 如果环境变量指定配置文件路径则优先使用
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		if data, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(data, &cfg)
		}
	} else {
		// 2) 否则按候选路径搜索
		candidates := []string{"config.json"}

		if exePath, err := os.Executable(); err == nil {
			candidates = append(candidates, filepath.Join(filepath.Dir(exePath), "config.json"))
		}
		candidates = append(candidates, "/etc/check_mount_daemon/config.json")

		for _, c := range candidates {
			if data, err := os.ReadFile(c); err == nil {
				_ = json.Unmarshal(data, &cfg)
				break
			}
		}
	}

	// 环境变量覆盖 sourceDir
	if v := os.Getenv("SOURCE_DIR"); v != "" {
		cfg.SourceDir = v
	}

	// 如果仍然为空，使用回退默认值
	if cfg.SourceDir == "" {
		cfg.SourceDir = "/vol02/1000-0-122ea9fa/Photos/"
	}

	// 环境变量覆盖
	if v := os.Getenv("SOURCE_DIR"); v != "" {
		cfg.SourceDir = v
	}

	// 如果仍然为空，使用回退默认值
	if cfg.SourceDir == "" {
		cfg.SourceDir = "/vol02/1000-0-122ea9fa/Photos/"
	}

	return cfg
}

func main() {
	cfg := loadConfig()
	for {
		checkAndMount(cfg.SourceDir)
		time.Sleep(interval)
	}
}

func checkAndMount(sourceDir string) {
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
	targetFile := targetDir + checkFile
	if _, err := os.Stat(targetFile); err == nil {
		fmt.Printf("[%s] 挂载正常，无需操作。\n", time.Now().Format(time.RFC3339))
		return
	}

	// 挂载动作
	fmt.Printf("[%s] 未检测到 %s，执行挂载...\n", time.Now().Format(time.RFC3339), targetFile)
	// 检查是否具有执行 mount 的权限（Unix 需 root）
	if !isRoot() && runtime.GOOS != "windows" {
		fmt.Printf("[%s] 挂载需要超级用户权限，请以 root 或使用 sudo 运行程序。\n", time.Now().Format(time.RFC3339))
		return
	}

	cmd := exec.Command("mount", "--bind", sourceDir, targetDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("[%s] 挂载失败: %v\n输出: %s\n", time.Now().Format(time.RFC3339), err, string(output))
		return
	}
	fmt.Printf("[%s] 挂载成功！\n", time.Now().Format(time.RFC3339))
}

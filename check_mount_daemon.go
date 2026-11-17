package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultTargetDir = "/vol1/1000/Photos"
	defaultCheckFile = "remote"
	defaultSourceDir = "/vol02/1000-0-122ea9fa/Photos"
	defaultInterval  = 30 * time.Second
	minimumInterval  = 5 * time.Second
	maximumInterval  = 24 * time.Hour
)

// Config 保存应用运行所需的挂载配置。
// SourceDir 保留用于兼容早期只有 sourceDir 的配置文件。
type Config struct {
	SourceDir      string  `json:"sourceDir,omitempty"`
	Mounts         []Mount `json:"mounts,omitempty"`
	IntervalSecond int     `json:"intervalSeconds,omitempty"`
}

// Mount 表示一个挂载任务。
type Mount struct {
	Source    string `json:"source"`
	Target    string `json:"target"`
	CheckFile string `json:"checkFile,omitempty"`
}

type fileSignature struct {
	Exists  bool
	Size    int64
	ModTime time.Time
	Digest  [sha256.Size]byte
}

// isRoot 检查当前进程是否以 root 权限运行（仅在类 Unix 系统有效）。
func isRoot() bool {
	if runtime.GOOS == "windows" {
		return true
	}
	return os.Geteuid() == 0
}

func (c Config) interval() time.Duration {
	if c.IntervalSecond <= 0 {
		return defaultInterval
	}
	d := time.Duration(c.IntervalSecond) * time.Second
	if d < minimumInterval {
		return minimumInterval
	}
	if d > maximumInterval {
		return maximumInterval
	}
	return d
}

// effectiveMounts 把旧格式转换成一个挂载项，并补全可选字段。
func (c Config) effectiveMounts() []Mount {
	mounts := make([]Mount, 0, len(c.Mounts)+1)
	for _, mount := range c.Mounts {
		mount.Source = strings.TrimSpace(mount.Source)
		if mount.Source == "" {
			continue
		}
		mount.Target = strings.TrimSpace(mount.Target)
		if mount.Target == "" {
			mount.Target = defaultTargetDir
		}
		mount.CheckFile = strings.TrimSpace(mount.CheckFile)
		if mount.CheckFile == "" {
			mount.CheckFile = defaultCheckFile
		}
		mounts = append(mounts, mount)
	}
	if len(mounts) == 0 && strings.TrimSpace(c.SourceDir) != "" {
		mounts = append(mounts, Mount{
			Source:    strings.TrimSpace(c.SourceDir),
			Target:    defaultTargetDir,
			CheckFile: defaultCheckFile,
		})
	}
	return mounts
}

func configCandidates() []string {
	candidates := []string{"config.json"}
	if exePath, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exePath), "config.json"))
	}
	candidates = append(candidates, "/etc/check_mount_daemon/config.json")
	return candidates
}

// loadConfig 从 CONFIG_PATH 或标准候选路径加载配置。
func loadConfig() (Config, string, error) {
	cfg := Config{}
	configPath := ""

	paths := configCandidates()
	if configuredPath := strings.TrimSpace(os.Getenv("CONFIG_PATH")); configuredPath != "" {
		paths = []string{configuredPath}
	}

	var lastErr error
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			lastErr = err
			continue
		}
		configPath = path
		if err := json.Unmarshal(data, &cfg); err != nil {
			return cfg, configPath, fmt.Errorf("parse config %s: %w", path, err)
		}
		lastErr = nil
		break
	}

	if configPath == "" && lastErr != nil && os.Getenv("CONFIG_PATH") != "" {
		configuredPath := strings.TrimSpace(os.Getenv("CONFIG_PATH"))
		return cfg, configuredPath, fmt.Errorf("read config %s: %w", configuredPath, lastErr)
	}
	if v := strings.TrimSpace(os.Getenv("SOURCE_DIR")); v != "" {
		cfg.SourceDir = v
	}
	if cfg.SourceDir == "" && len(cfg.Mounts) == 0 {
		cfg.SourceDir = defaultSourceDir
	}
	return cfg, configPath, nil
}

func signature(path string) fileSignature {
	if path == "" {
		return fileSignature{}
	}
	info, err := os.Stat(path)
	if err != nil {
		return fileSignature{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fileSignature{}
	}
	return fileSignature{Exists: true, Size: info.Size(), ModTime: info.ModTime(), Digest: sha256.Sum256(data)}
}

func logf(format string, args ...interface{}) {
	fmt.Printf("[%s] %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

func main() {
	cfg, configPath, err := loadConfig()
	if err != nil {
		logf("加载配置失败: %v", err)
		// 配置文件损坏时保留可运行的旧默认行为，等待用户通过设置页修复配置。
		if cfg.SourceDir == "" && len(cfg.Mounts) == 0 {
			cfg.SourceDir = defaultSourceDir
		}
	}
	if configPath != "" {
		logf("使用配置文件: %s", configPath)
	} else {
		logf("未找到配置文件，使用内置默认配置")
	}
	webConfigPath := configPath
	if webConfigPath == "" {
		webConfigPath = strings.TrimSpace(os.Getenv("CONFIG_PATH"))
	}
	if webConfigPath == "" {
		webConfigPath = "/etc/check_mount_daemon/config.json"
	}
	go func() {
		if err := serveConfigWeb(webConfigPath); err != nil {
			logf("配置网页启动失败: %v", err)
		}
	}()

	lastSignature := signature(configPath)
	for {
		mounts := cfg.effectiveMounts()
		if len(mounts) == 0 {
			logf("没有可用的挂载项，请在应用设置中配置源目录")
		}
		for _, mount := range mounts {
			checkAndMountSingle(mount.Source, mount.Target, mount.CheckFile)
		}

		time.Sleep(cfg.interval())
		nextCfg, nextPath, nextErr := loadConfig()
		if nextErr != nil {
			if nextPath != configPath || signature(nextPath) != lastSignature {
				logf("重新加载配置失败，继续使用上一份配置: %v", nextErr)
			}
			continue
		}
		nextSignature := signature(nextPath)
		if nextPath != configPath || nextSignature != lastSignature {
			cfg = nextCfg
			configPath = nextPath
			lastSignature = nextSignature
			logf("配置已重新加载")
		}
	}
}

func checkAndMountSingle(sourceDir, targetDirArg, checkFileArg string) {
	sourceDir = strings.TrimSpace(sourceDir)
	targetDirLocal := strings.TrimSpace(targetDirArg)
	checkFileLocal := strings.TrimSpace(checkFileArg)
	if targetDirLocal == "" {
		targetDirLocal = defaultTargetDir
	}
	if checkFileLocal == "" {
		checkFileLocal = defaultCheckFile
	}
	if sourceDir == "" {
		logf("源目录为空，跳过")
		return
	}

	sourceInfo, err := os.Stat(sourceDir)
	if err != nil {
		if os.IsNotExist(err) {
			logf("源目录 %s 不存在，跳过", sourceDir)
		} else {
			logf("读取源目录 %s 失败: %v", sourceDir, err)
		}
		return
	}
	if !sourceInfo.IsDir() {
		logf("源路径 %s 不是目录，跳过", sourceDir)
		return
	}

	// 源目录必须有标记文件，避免目标目录已经挂载后每个周期反复执行 mount。
	sourceFile := filepath.Join(sourceDir, checkFileLocal)
	if _, err := os.Stat(sourceFile); err != nil {
		if os.IsNotExist(err) {
			logf("源目录缺少检查文件 %s，跳过", sourceFile)
		} else {
			logf("读取检查文件 %s 失败: %v", sourceFile, err)
		}
		return
	}

	if targetInfo, err := os.Stat(targetDirLocal); err != nil {
		if !os.IsNotExist(err) {
			logf("读取目标目录 %s 失败: %v", targetDirLocal, err)
			return
		}
		logf("目标目录 %s 不存在，正在创建...", targetDirLocal)
		if err := os.MkdirAll(targetDirLocal, 0755); err != nil {
			logf("创建目标目录失败: %v", err)
			return
		}
	} else if !targetInfo.IsDir() {
		logf("目标路径 %s 不是目录，跳过", targetDirLocal)
		return
	}

	mounted, mountedFrom := mountedSource(targetDirLocal)
	if mounted && !sameMountSource(mountedFrom, sourceDir) {
		logf("目标目录已被其他挂载占用 (%s)，跳过", mountedFrom)
		return
	}

	targetFile := filepath.Join(targetDirLocal, checkFileLocal)
	if _, err := os.Stat(targetFile); err == nil {
		if mounted {
			logf("挂载 %s -> %s 正常，无需操作", sourceDir, targetDirLocal)
			return
		}
		logf("目标目录存在检查文件但当前未挂载，准备恢复挂载 %s -> %s", sourceDir, targetDirLocal)
	} else if !os.IsNotExist(err) {
		logf("读取目标检查文件 %s 失败: %v", targetFile, err)
		return
	}

	logf("未检测到 %s，准备挂载 %s -> %s", targetFile, sourceDir, targetDirLocal)
	if !isRoot() && runtime.GOOS != "windows" {
		logf("挂载需要超级用户权限，请以 root 运行应用")
		return
	}

	if mounted {
		// 这是本程序之前建立的失效 bind mount，先卸载再恢复。
		output, err := exec.Command("umount", targetDirLocal).CombinedOutput()
		if err != nil {
			logf("卸载失效挂载失败: %v，输出: %s", err, strings.TrimSpace(string(output)))
			return
		}
	}

	output, err := exec.Command("mount", "--bind", sourceDir, targetDirLocal).CombinedOutput()
	if err != nil {
		logf("挂载失败: %v，输出: %s", err, strings.TrimSpace(string(output)))
		return
	}
	logf("挂载成功: %s -> %s", sourceDir, targetDirLocal)
}

func sameMountSource(mountedFrom, source string) bool {
	mountedFrom = filepath.Clean(strings.TrimSpace(mountedFrom))
	source = filepath.Clean(strings.TrimSpace(source))
	if mountedFrom == source {
		return true
	}
	// findmnt displays bind sources on some util-linux versions as
	// /dev/volume[/path/to/source].
	return strings.HasSuffix(mountedFrom, "["+source+"]")
}

// mountedSource 返回目标目录是否为挂载点以及挂载来源。findmnt 在 fnOS/Debian 上随 util-linux 提供。
func mountedSource(target string) (bool, string) {
	cmd := exec.Command("findmnt", "-n", "-o", "TARGET,SOURCE", "--target", target)
	output, err := cmd.Output()
	if err != nil {
		return false, ""
	}
	fields := strings.Fields(strings.TrimSpace(string(output)))
	if len(fields) < 2 || filepath.Clean(fields[0]) != filepath.Clean(target) {
		return false, ""
	}
	return true, fields[1]
}

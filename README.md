# check_mount_daemon

将飞牛远程目录通过 `bind mount` 挂载到本地相册目录，例如 `/vol1/1000/Photos`，解决系统盘空间不足时无法继续扩展相册目录的问题。

项目现在已经补齐：

- `.deb` 安装包构建
- 飞牛 `Native` 应用 `.fpk` 打包（x86/ARM 架构校验）
- 飞牛应用中心安装向导和应用设置中的可视化配置
- `systemd` 服务安装与开机自启
- GitHub Actions 自动发布到 GitHub Releases

## 安装后文件位置

- 二进制文件：`/usr/bin/check_mount_daemon`
- 配置文件：`/etc/check_mount_daemon/config.json`
- 服务文件：`/lib/systemd/system/check-mount.service`

## 配置示例

安装后编辑 `/etc/check_mount_daemon/config.json`：

```json
{
  "mounts": [
    {
      "source": "/vol02/1000-0-1fd91f9b/Picture",
      "target": "/vol1/1000/Photos",
      "checkFile": "remote"
    },
    {
      "source": "/vol02/1001-1-750eb99a/Photos",
      "target": "/vol1/1001/Photos",
      "checkFile": "remote"
    }
  ],
  "intervalSeconds": 30
}
```

说明：

- `source`：远程挂载目录
- `target`：飞牛本地相册目录
- `checkFile`：用于判断挂载是否正常的标记文件，默认值为 `remote`
- `intervalSeconds`：检查间隔，范围为 5 到 86400 秒，默认 30 秒

## 使用前准备

在每个 `source` 根目录下创建检查文件，例如：

```bash
touch /vol02/1000-0-122ea9fa/Photos/remote
```

程序会定期检查 `target/checkFile` 是否存在；如果不存在且源目录中的检查文件存在，就执行 `mount --bind source target`。目标目录已经是挂载点时会先确认挂载来源，避免重复叠加挂载。配置文件修改后会在下一个检查周期自动重新加载。

## 直接安装 `.deb`

从 GitHub Releases 下载对应架构的安装包后执行：

```bash
sudo dpkg -i check-mount-daemon_<version>_<arch>.deb
sudo systemctl status check-mount.service
```

安装脚本会自动：

- 安装二进制和默认配置
- 注册 `check-mount.service`
- 执行 `systemctl enable`
- 自动启动或重启服务

常用命令：

```bash
sudo systemctl restart check-mount.service
sudo systemctl stop check-mount.service
sudo systemctl status check-mount.service
journalctl -u check-mount.service -f
```

## 安装飞牛应用 `.fpk`

仓库现在也会生成飞牛 `Native` 应用包，适合手动安装到 fnOS 上测试：

```bash
appcenter-cli install-fpk check-mount-daemon_<version>_fnos_x86.fpk --volume 1 --env /path/to/check-mount.env
```

命令行安装时需要提供向导变量；通过应用中心网页安装时会直接显示同样的表单。`check-mount.env` 示例：

```text
wizard_source_dir_1=/vol02/1000-0-122ea9fa/Photos
wizard_target_dir_1=/vol1/1000/Photos
wizard_check_file_1=remote
wizard_interval_seconds=30
```

或者先开启手动安装入口，再通过应用中心页面上传安装包：

```bash
appcenter-cli manual-install enable
```

说明：

- `fpk` 包是服务型应用，安装时会显示挂载配置向导；安装后进入应用中心的“应用设置”即可修改源目录、目标目录、检查文件和检查间隔
- 可视化配置最多提供 3 个挂载项；更多挂载项仍可直接编辑 `etc/config.json`
- 应用会以 `root` 权限运行，因为 `mount --bind` 本身需要特权
- 首次安装会把默认配置复制到应用的 `etc/config.json`
- 源目录必须存在，并在根目录创建对应的检查文件，例如 `touch /vol02/1000-0-122ea9fa/Photos/remote`

应用设置中的“保存并重启”会执行配置回调，守护进程随后自动恢复运行。

## 本地构建 `.deb`

建议在 Linux 或 WSL 中执行：

```bash
go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean --skip=publish
```

构建产物会输出到 `dist/` 目录。

## 本地构建 `.fpk`

1. 下载官方 `fnpack`

```bash
curl -L -o fnpack https://static2.fnnas.com/fnpack/fnpack-1.2.1-linux-amd64
chmod +x fnpack
```

2. 先编译与飞牛设备匹配的 Linux 二进制，再打包。`--platform arm` 必须使用 `GOARCH=arm64`，`--platform x86` 必须使用 `GOARCH=amd64`；脚本会在打包前检查 ELF 架构：

```bash
GOOS=linux GOARCH=amd64 go build -o dist/check_mount_daemon ./check_mount_daemon.go
go run ./scripts/build_fpk.go \
  --fnpack ./fnpack \
  --binary dist/check_mount_daemon \
  --version 1.0.1 \
  --platform x86 \
  --stage dist/fpk-x86 \
  --output dist/check-mount-daemon_1.0.1_fnos_x86.fpk
```

ARM 飞牛设备（`uname -m` 通常为 `aarch64`）使用同样流程，但编译时改为 `GOARCH=arm64`，并将平台改为 `arm`：

```bash
GOOS=linux GOARCH=arm64 go build -o dist/check_mount_daemon ./check_mount_daemon.go
go run ./scripts/build_fpk.go \
  --fnpack ./fnpack \
  --binary dist/check_mount_daemon \
  --version 1.0.1 \
  --platform arm \
  --stage dist/fpk-arm \
  --output dist/check-mount-daemon_1.0.1_fnos_arm.fpk
```

## 自动发布到 GitHub Releases

仓库已经配置 `.github/workflows/release.yml`。只要创建并推送版本标签，就会自动构建：

- Linux `amd64` 二进制包
- Linux `arm64` 二进制包
- `amd64` / `arm64` 的 `.deb` 安装包
- `x86` / `arm` 的飞牛 `.fpk` 安装包
- `checksums.txt`

发布方式：

```bash
git tag v1.1.0
git push origin v1.1.0
```

工作流会自动创建对应的 GitHub Release 并上传产物。

# check_mount_daemon

将飞牛远程目录通过 `bind mount` 挂载到本地相册目录，例如 `/vol1/1000/Photos`，解决系统盘空间不足时无法继续扩展相册目录的问题。

项目现在已经补齐：

- `.deb` 安装包构建
- 飞牛 `Native` 应用 `.fpk` 打包
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
  ]
}
```

说明：

- `source`：远程挂载目录
- `target`：飞牛本地相册目录
- `checkFile`：用于判断挂载是否正常的标记文件，默认值为 `remote`

## 使用前准备

在每个 `source` 根目录下创建检查文件，例如：

```bash
touch /vol02/1000-0-122ea9fa/Photos/remote
```

程序会定期检查 `target/checkFile` 是否存在；如果不存在，就重新执行 `mount --bind source target`。

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
appcenter-cli install-fpk check-mount-daemon_<version>_fnos_x86.fpk
```

或者先开启手动安装入口，再通过应用中心页面上传安装包：

```bash
appcenter-cli manual-install enable
```

说明：

- `fpk` 包内置为无 UI 的服务型应用
- 应用会以 `root` 权限运行，因为 `mount --bind` 本身需要特权
- 首次安装会把默认配置复制到应用的 `etc/config.json`
- 该形态更适合自用或测试安装，不适合作为普通第三方应用上架应用中心

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

2. 先编译 Linux 二进制，再打包：

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

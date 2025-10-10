# 项目简介
自动在飞牛“我的文件”目录下创建远程挂载目录的软链接，以解决安装盘空间过小无法备份相册问题

编译：  
```bash
go build -o check_mount_daemon check_mount_daemon.go
```

将编译后文件以及配置文件放置于/usr/local/bin  

修改config.json配置文件  
```json
{
  "sourceDir": "/vol02/1000-0-122ea9fa/Photos/"
}
```

新建服务文件: /etc/systemd/system/check-mount.service  
```ini
[Unit]
Description=Auto check and bind mount fnos-data
After=network-online.target

[Service]
ExecStart=/usr/local/bin/check_mount_daemon
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target

```

启动并设置开机自启：  
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now check-mount.service
```


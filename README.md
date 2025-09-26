编译：  
```bash
go build -o check_mount_daemon check_mount_daemon.go
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


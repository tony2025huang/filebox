# FileBox 部署

## Linux systemd

1. 创建专用用户和目录：

   ```bash
   sudo useradd --system --home /opt/filebox --shell /usr/sbin/nologin filebox
   sudo install -d -o filebox -g filebox /opt/filebox /var/lib/filebox /var/log/filebox
   sudo install -o filebox -g filebox filebox /opt/filebox/filebox
   sudo install -m 0644 deploy/filebox.service /etc/systemd/system/filebox.service
   ```

2. 将 unit 中的 `FILEBOX_JWT_SECRET=CHANGE_ME` 换成强随机值。确认 `filebox` 用户拥有数据和日志目录，然后启用服务：

   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable --now filebox
   sudo systemctl status filebox
   journalctl -u filebox -f
   ```

服务日志默认写入 `/var/log/filebox/filebox-YYYY-MM-DD.log`，并保留控制台/journal 输出。升级时先停止服务，用新二进制替换 `/opt/filebox/filebox`，再执行 `systemctl restart filebox`。

## Windows 服务

管理员 PowerShell 推荐使用 NSSM。将 `nssm.exe` 放入 `PATH`，准备强随机 JWT secret，然后运行：

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\deploy\install-service.ps1 -Executable C:\Program Files\FileBox\filebox.exe -JwtSecret 'replace-with-a-random-secret'
nssm start FileBox
```

脚本会创建专用数据/日志目录，并把服务输出重定向到日志目录。卸载：

```powershell
nssm stop FileBox
nssm remove FileBox confirm
```

没有 NSSM 时脚本会使用 `sc.exe create` 的基础方案，并输出 `sc.exe start/stop/delete` 命令。该回退方案不能将 secret 写入服务环境，请在启动前配置机器级 `FILEBOX_JWT_SECRET`：

```powershell
[Environment]::SetEnvironmentVariable('FILEBOX_JWT_SECRET', 'replace-with-a-random-secret', 'Machine')
sc.exe start FileBox
```

## Nginx 反向代理

先构建前端并将 `web/dist` 部署到示例配置的 `root` 目录，复制 `nginx.conf.example` 到 Nginx 配置目录，替换域名和 TLS 证书路径：

```bash
sudo nginx -t
sudo systemctl reload nginx
```

FileBox 监听 `127.0.0.1:8080` 时使用 `--trusted-proxies=127.0.0.1/32`。多级代理必须把实际代理网段加入可信列表；不要把公网网段配置为可信代理，否则客户端可以伪造 `X-Forwarded-For` 并绕过 IP 锁定或白名单。示例关闭 Nginx 请求缓冲、取消上传体积限制并设置 600 秒读取超时，实际大小仍由 FileBox 的 `--max-file-size` 控制。生产环境必须使用 HTTPS。

部署建议：独立运行用户、独立数据/日志目录、强随机 `FILEBOX_JWT_SECRET`、启用服务日志并记录归档位置。升级时保留数据目录，停止服务后替换二进制，再启动并检查日志。

# FileBox Deployment

## Linux systemd

1. Create the dedicated user and directories:

   ```bash
   sudo useradd --system --home /opt/filebox --shell /usr/sbin/nologin filebox
   sudo install -d -o filebox -g filebox /opt/filebox /var/lib/filebox /var/log/filebox
   sudo install -o filebox -g filebox filebox /opt/filebox/filebox
   sudo install -m 0644 deploy/filebox.service /etc/systemd/system/filebox.service
   ```

2. Replace `FILEBOX_JWT_SECRET=CHANGE_ME` in the unit with a strong random secret. Verify that the `filebox` user owns the data and log directories, then enable the service:

   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable --now filebox
   sudo systemctl status filebox
   journalctl -u filebox -f
   ```

Service logs are written to `/var/log/filebox/filebox-YYYY-MM-DD.log` and remain available through the console/journal. To upgrade, stop the service, replace `/opt/filebox/filebox`, and run `systemctl restart filebox`.

## Windows service

NSSM is recommended from an elevated PowerShell. Put `nssm.exe` on `PATH`, prepare a strong random JWT secret, and run:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\deploy\install-service.ps1 -Executable 'C:\Program Files\FileBox\filebox.exe' -JwtSecret 'replace-with-a-random-secret'
nssm start FileBox
```

The script creates dedicated data/log directories and redirects service stdout/stderr there. To remove the service:

```powershell
nssm stop FileBox
nssm remove FileBox confirm
```

Without NSSM the script uses the basic `sc.exe create` fallback and prints `sc.exe start/stop/delete` commands. The fallback cannot put the secret into the service environment, so configure a machine-level `FILEBOX_JWT_SECRET` before starting it:

```powershell
[Environment]::SetEnvironmentVariable('FILEBOX_JWT_SECRET', 'replace-with-a-random-secret', 'Machine')
sc.exe start FileBox
```

## Nginx reverse proxy

Build the frontend and deploy `web/dist` to the `root` path in the example configuration. Copy `nginx.conf.example` into the Nginx configuration directory and replace the hostname and TLS certificate paths:

```bash
sudo nginx -t
sudo systemctl reload nginx
```

When FileBox listens on `127.0.0.1:8080`, configure `--trusted-proxies=127.0.0.1/32`. For multiple proxy hops, list the real proxy networks. Never trust public networks: clients could forge `X-Forwarded-For` and bypass IP lockout or allowlists. The example disables Nginx request buffering, removes the proxy body limit, and sets a 600-second read timeout; FileBox `--max-file-size` remains the effective upload limit. Production deployments must use HTTPS.

Recommended practice is a dedicated service account, separate data/log directories, a strong random `FILEBOX_JWT_SECRET`, enabled service logs with documented archive location, and a stop/replace/start upgrade procedure.

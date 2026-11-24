# 測試映像

`Dockerfile.test-idp` 將由目前來源交叉建置的測試 IdP 放入非 root scratch 映像；只供隔離 fixture 使用。

`Dockerfile.m6-runner` 從固定 digest 的 Go 1.27.4 Bookworm 與 PostgreSQL 18.7 Bookworm 組合驗收環境。只加入 `pg_dump`、`pg_restore`、`psql` 及其 `libpq`，不下載浮動套件；建置時實際執行三個工具的版本檢查。其用途是讓 Linux integration／race 與發布演練原生執行真實備份還原，不需要將 host Docker socket 提供給 runner。

從根目錄以本次隔離 project 命名映像：

```powershell
docker build -f deploy/docker/Dockerfile.m6-runner -t pangolin-m6-dev-runner .
```

映像帶有 `dev.pangolin.owner=pangolin-framework`、`dev.pangolin.purpose=m6-test-runner`。驗收完成後確認沒有 container 使用，再移除本次專用映像；基底共用映像與其他 project 不清除。原始命令、版本、映像 ID 及 Gate logs 保留於 workspace artifacts。

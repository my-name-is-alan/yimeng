# yimeng core

Go 版 Core（纯 Go，CGO-free，SQLite 用 modernc.org/sqlite）。

```sh
YIMENG_LIBRARY_ROOTS=$HOME/Music YIMENG_DB_PATH=./data/library.db YIMENG_CACHE_DIR=./data/cache \
  go run ./cmd/core
curl localhost:8741/healthz
curl -XPOST localhost:8741/api/library/scan            # {"root": "...", "full": true} optional
curl localhost:8741/api/library/scan/<id>              # or /api/library/scan for latest
```

- 配置：`configs/default.toml` + `YIMENG_CONFIG`；环境变量覆盖（见 `internal/infra/config`）。
- 迁移：`migrations/NNNN_*.sql` 嵌入二进制，启动时按版本执行（`schema_migrations`）。
- 部署：`make dist`（linux amd64/arm64 静态二进制）、`make docker`（buildx 多架构），`compose.example.yaml`。

详见 ../docs/core-architecture.md

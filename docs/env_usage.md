# CloudVault `.env` 使用说明

项目已支持在启动时自动加载根目录的：
- `.env`
- `.env.local`

加载规则：
- 不覆盖系统环境变量（例如你在终端里 `$env:ES_ENABLED='false'`，会优先于 `.env`）。
- 优先级：`系统环境变量` > `.env.local` > `.env` > 代码默认值。

建议：
- 把可共享配置写在 `.env.example`。
- 把你本机真实配置写在 `.env` 或 `.env.local`。

## 快速使用

1. 复制模板：

```powershell
Copy-Item .env.example .env
```

2. 修改 `.env` 中你本地的服务地址和账号。

3. 启动：

```powershell
go run .
```

`go run ./cmd/worker` 同样会自动读取 `.env`。

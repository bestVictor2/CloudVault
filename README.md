# CloudVault

CloudVault 是一个前后端分离的个人网盘系统，覆盖上传、下载、分享、回收站、离线下载、预览与用户行为统计等核心能力。  
后端基于 Go + Gin，前端为静态页面，适合作为个人项目、课程设计或网盘类系统的基础模板。

## 项目状态

- 当前可用: 核心网盘能力已可跑通
- 架构形态: 单体服务 + 异步 Worker
- 维护方向: 继续补齐存储集群、分库分表、检索与预览增强

## 功能概览

| 模块 | 已实现能力 |
| --- | --- |
| 认证与用户 | 注册、邮箱激活、登录、JWT 鉴权、个人资料读写 |
| 文件管理 | 列表/搜索、重命名、移动、复制、建目录、批量删除 |
| 上传能力 | 秒传、分片上传（断点续传）、URL 导入上传 |
| 下载能力 | 预签名下载、流式下载、ZIP 打包下载 |
| 回收站 | 列表、恢复、彻底删除（含对象引用计数清理） |
| 分享能力 | 创建分享、提取码、过期失效、公开下载 |
| 离线下载 | RabbitMQ 队列、失败重试、限速与并发控制 |
| 用户内容扩展 | 收藏、最近访问、常用目录 |
| 行为统计 | 活动事件流、分享访问日志、来源统计、汇总接口 |
| 安全防护 | SSRF/Zip Slip/CRLF 防护、排序字段白名单、JWT 算法校验 |

## 技术栈

| 层 | 技术 |
| --- | --- |
| 后端 | Go 1.24, Gin, GORM |
| 存储 | MySQL, Redis, MinIO |
| 异步任务 | RabbitMQ, Worker |
| 认证 | JWT |
| 前端 | 静态页面 (`static/`) |

## 目录结构

```text
CloudVault/
├─ cmd/                    # 可执行入口（worker）
├─ config/                 # 配置与环境变量
├─ internal/               # 业务实现（handler/service/repo/worker）
├─ model/                  # 数据模型
├─ router/                 # 路由定义
├─ static/                 # 前端页面与静态资源
├─ test/                   # 测试代码
├─ main.go                 # API 服务入口
└─ README.md
```

## 快速开始

### 1. 依赖准备

- Go `1.24+`（项目使用 `toolchain go1.24.6`）
- MySQL
- Redis
- MinIO
- RabbitMQ

### 2. 配置环境变量

先配置最常用项（其余可使用默认值）:

| 组件 | 环境变量 | 默认值 |
| --- | --- | --- |
| JWT | `JWT_SECRET` | `l=ax+b` |
| MySQL | `DB_HOST/DB_PORT/DB_USER/DB_PASS/DB_NAME` | `localhost/3306/root/root/Go_Pan` |
| Redis | `REDIS_HOST/REDIS_PORT/REDIS_PASSWORD` | `localhost/6379/(空)` |
| MinIO | `MINIO_HOST/MINIO_PORT/MINIO_USERNAME/MINIO_PASSWORD/BUCKET_NAME` | `localhost/9000/minioadmin/minioadmin/netdisk` |
| RabbitMQ | `RABBITMQ_URL` 或 `RABBITMQ_HOST/PORT/USER/PASSWORD/VHOST` | 自动拼装或 `localhost/5672/guest/guest//` |

离线下载相关可选参数:

- `DOWNLOAD_WORKER_CONCURRENCY` (默认 `4`)
- `DOWNLOAD_RATE` (默认 `2`)
- `DOWNLOAD_BURST` (默认 `4`)
- `DOWNLOAD_RETRY_MAX` (默认 `5`)
- `DOWNLOAD_RETRY_DELAYS` (默认 `10s,30s,2m,10m,30m`)
- `DOWNLOAD_HTTP_TIMEOUT` (默认 `30m`)
- `DOWNLOAD_ALLOW_PRIVATE` (默认 `false`)
- `DOWNLOAD_ALLOW_HOSTS` (逗号分隔白名单)
- `DOWNLOAD_MAX_BYTES` (默认 `0` 表示不限制)

### 3. 启动 API 服务

```powershell
$env:GO111MODULE='on'
go run .
```

默认监听 `:8000`，API 基地址为 `http://localhost:8000/api`。

### 4. 启动 Worker

```powershell
$env:GO111MODULE='on'
go run ./cmd/worker
```

当前会同时启动:

- 下载任务 Worker (`download.queue`)
- 活动统计 Worker (`activity.queue`)

### 5. 访问前端

直接打开 `static/index.html`，将 API Base 设置为 `http://localhost:8000/api`。

常用页面:

- `static/pages/files.html`
- `static/pages/upload.html`
- `static/pages/recycle.html`
- `static/pages/share.html`
- `static/pages/tasks.html`
- `static/pages/profile.html`
- `static/pages/library.html`
- `static/pages/share-analytics.html`

## 核心接口一览

| 模块 | 路由 |
| --- | --- |
| 认证 | `POST /api/register`, `GET /api/activate`, `POST /api/login` |
| 文件 | `POST /api/file/list`, `POST /api/file/search`, `POST /api/file/rename`, `POST /api/file/move`, `POST /api/file/copy` |
| 上传 | `POST /api/file/upload/hash`, `POST /api/file/upload/url`, `POST /api/file/upload/multipart/*` |
| 下载 | `POST /api/file/download/minio`, `POST /api/file/download/url`, `POST /api/file/download/archive` |
| 预览 | `GET /api/file/preview/:fileID` |
| 离线任务 | `POST /api/file/download/offline`, `GET /api/file/download/tasks` |
| 回收站 | `POST /api/recycle/list`, `POST /api/recycle/restore`, `POST /api/recycle/delete` |
| 分享 | `POST /api/share/create`, `GET /api/share/download/:shareID` |
| 分享统计 | `GET /api/share/access/logs`, `GET /api/share/access/stats` |
| 用户中心 | `GET /api/user/me`, `PUT /api/user/me` |
| 内容扩展 | `GET/POST/DELETE /api/user/favorites`, `GET /api/user/recent`, `GET /api/user/common-dirs` |
| 活动汇总 | `GET /api/user/activity/summary?days=7` |

## 测试

确保 MySQL、Redis、MinIO、RabbitMQ 均可用后执行:

```powershell
$env:GO111MODULE='on'
go test ./...
```

项目会在启动时执行 AutoMigrate，无需手动建表。

## 注意事项

- Redis 过期事件依赖 `notify-keyspace-events`，程序会尝试自动开启（需要 `CONFIG SET` 权限）。
- 分享过期与离线下载重试逻辑依赖 Redis/RabbitMQ/Worker 常驻。
- 当前主链路默认单 MinIO，存储集群能力仍在演进中。

## 后续规划

- 存储集群主链路切换与迁移能力完善
- 分库分表在业务层全面落地
- 搜索增强（全文检索）与预览增强（转码）
- 任务中心可视化与可操作性提升
- 配额、权限模型、版本管理等企业级能力扩展

## License

[MIT](LICENSE)

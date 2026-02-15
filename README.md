# Go_Pan

Go_Pan 是一个前后端分离的个人网盘系统，覆盖上传、下载、分享、回收站、离线下载、搜索与预览等核心能力，项目目前仅支持普通功能，仍在完善中

## 已完成的功能（详细）
### 认证与用户
- 注册、邮箱激活(暂时只支持QQ邮箱，待扩展)、登录
- JWT 鉴权与统一鉴权中间件 
- 登录需账号激活，注册需两次密码一致
### 文件管理
- 文件列表（分页/排序）、名称搜索
- 重命名、移动、复制（含目录递归复制）
- 创建文件夹（支持嵌套）、批量删除
- 回收站列表、恢复、彻底删除（含对象引用计数与物理清理） 
### 上传文件
- 秒传（hash 复用 + 引用计数 + MinIO 对象存在性检查）
- 分片上传（初始化/分片/合并、断点续传已上传分片、Redis 锁避免并发合并）
- URL 导入上传（安全校验后落库/落存储）
### 下载与预览
- MinIO 直链下载（预签名 URL）
- 直传下载（流式响应）
- 打包下载（ZIP），目录与文件路径安全处理
### 离线下载任务
- RabbitMQ 任务队列 + Worker 消费
- 失败重试、重试延迟、限速与并发控制
- 任务状态、进度与任务列表查询
### 预览与分享
- 创建分享、提取码、过期时间
- Redis Keyspace 过期监听驱动的分享失效
- 预签名预览链接（`Content-Type` 与 `inline` 响应头）
### 安全&稳定性&性能
- SSRF 防护（私网/IP/Host 校验、重定向校验、Allowlist）
- CRLF Header 注入防护（响应头文件名清洗）
- Zip Slip 防护（ZIP 内路径清洗）
- 排序字段白名单（避免 SQL 注入式排序）
- JWT 签名算法校验（拒绝非 HS256）
- 分享提取码使用加密随机
- 资源限制（离线下载超时、最大体积、限速）
- 文件列表缓存（Redis），变更自动失效

## 技术栈
## 使用的技术（详细）
- 后端
- Go 1.24（`go.mod` 使用 `toolchain go1.24.6`）
- Gin（HTTP 路由与中间件）
- GORM + MySQL（数据持久化）
- Redis（缓存、Keyspace 事件、分布式锁）
- MinIO（S3 兼容对象存储、预签名 URL）
- RabbitMQ（离线下载任务队列）
- JWT（鉴权）
- 前端
- 纯静态页面（`static/`，`index.html` + `app.js` + `styles.css`）

## 目录结构
- 后端：`cmd/`、`config/`、`internal/`、`model/`、`router/`、`utils/`
- 前端：`static/`（`index.html`、`pages/`、`app.js`、`styles.css`）
- 测试：`test/`

## 快速开始

### 依赖
- Go 1.24+（`go.mod` 使用 `toolchain go1.24.6`）
- MySQL、Redis、MinIO、RabbitMQ

### 配置（环境变量）
以下为常用配置项与默认值：

- 认证
  - `JWT_SECRET`：`l=ax+b`

- MySQL
  - `DB_HOST`：`localhost`
  - `DB_PORT`：`3306`
  - `DB_USER`：`root`
  - `DB_PASS`：`root`
  - `DB_NAME`：`Go_Pan`
  - `DB_NAME_TEST`：`Go_Pan_Test`

- Redis
  - `REDIS_HOST`：`localhost`
  - `REDIS_PORT`：`6379`
  - `REDIS_PASSWORD`：空
  - `REDIS_DB`：当前实现固定为 `0`

- MinIO
  - `MINIO_HOST`：`localhost`
  - `MINIO_PORT`：`9000`
  - `MINIO_USERNAME`：`minioadmin`
  - `MINIO_PASSWORD`：`minioadmin`
  - `BUCKET_NAME`：`netdisk`
  - `BUCKET_NAME_TEST`：`go-pan-test`

- RabbitMQ
  - `RABBITMQ_URL`：空（为空时由下列项拼装）
  - `RABBITMQ_HOST`：`localhost`
  - `RABBITMQ_PORT`：`5672`
  - `RABBITMQ_USER`：`guest`
  - `RABBITMQ_PASSWORD`：`guest`
  - `RABBITMQ_VHOST`：`/`
  - `RABBITMQ_PREFETCH`：`8`

- 离线下载/限流
  - `DOWNLOAD_ALLOW_PRIVATE`：`false`
  - `DOWNLOAD_ALLOW_HOSTS`：空（逗号分隔）
  - `DOWNLOAD_MAX_BYTES`：`0`（不限制）
  - `DOWNLOAD_HTTP_TIMEOUT`：`30m`
  - `DOWNLOAD_RETRY_MAX`：`5`
  - `DOWNLOAD_RETRY_DELAYS`：`10s,30s,2m,10m,30m`
  - `DOWNLOAD_RATE`：`2`
  - `DOWNLOAD_BURST`：`4`
  - `DOWNLOAD_WORKER_CONCURRENCY`：`4`

### 运行后端
在项目根目录执行：

```powershell
$env:GO111MODULE='on'
go run .
```

服务默认监听 `:8000`，API 基地址为 `http://localhost:8000/api`。

### 运行离线下载 Worker
```powershell
$env:GO111MODULE='on'
go run ./cmd/worker
```

### 前端访问
打开 `static/index.html`，在页面中设置 API Base 为 `http://localhost:8000/api`。

### 运行测试
确保 MySQL、Redis、MinIO、RabbitMQ 均可用，并配置好测试库/测试桶后执行：

```powershell
$env:GO111MODULE='on'
go test ./...
```

## 待完成的功能
- 存储集群真正落地：已有节点抽象、复制上传、迁移监控，但主链路仍默认走单 MinIO
- 分库分表：已有分片管理器，业务层尚未全面切换到分片
- 搜索/预览增强：未接入全文检索与文件转码
- 待完成细节偏多，比如离线下载队列无法进行操作

## 备注
- Redis 过期事件监听依赖 `notify-keyspace-events`，程序会通过 `CONFIG SET` 自动启用（需权限）。
- 离线下载依赖 RabbitMQ 与 Worker 常驻运行。

## 未来可能拓展的功能
- 空间配额、容量统计、文件类型统计与使用报表
- 权限模型升级（多角色、多级目录权限、分享可见范围）
- 文件版本控制与历史恢复、回收站保留策略
- 多存储后端接入（S3/OSS/COS）与多区域容灾
- 任务中心增强（统一任务状态、失败告警、通知）
- 更丰富的预览能力（图片/文档/视频缩略图与转码）

## 感谢使用

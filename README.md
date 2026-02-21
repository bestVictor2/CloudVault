# Go_Pan

Go_Pan 是一个前后端分离的个人网盘系统，覆盖上传、下载、分享、回收站、离线下载、搜索与预览等核心能力，项目目前仅支持普通功能，仍在完善中

## 已完成的功能（详细）
### 认证与用户
- 注册、邮箱激活(暂时只支持QQ邮箱，待扩展)、登录
- JWT 鉴权与统一鉴权中间件 
- 登录需账号激活，注册需两次密码一致
- 个人资料查询与编辑：`GET /api/user/me`、`PUT /api/user/me`
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
### 分布式横向扩展（新增）
- 用户行为事件流：业务写入事件到 RabbitMQ，Worker 异步消费
- 行为统计双写：MySQL 持久化日统计 + Redis 实时计数
- 用户活动统计接口：`GET /api/user/activity/summary?days=7`
- 分享访问日志与来源统计：`GET /api/share/access/logs`、`GET /api/share/access/stats`
- 公开分享下载链路埋点：记录 `IP/UserAgent/Referer/Source`，支持统计分析
### 内容横向扩展（新增）
- 收藏：`GET /api/user/favorites`、`POST /api/user/favorites`、`DELETE /api/user/favorites/:fileID`
- 最近访问：`GET /api/user/recent`
- 常用目录：`GET /api/user/common-dirs`
- 前端新增页面：`个人中心(可编辑)`、`内容扩展`、`分享统计`
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
以下为常用配置项与默认值：在此项目中，环境变量的配置有的使用了config中的环境变量，有的使用了系统自带的环境变量，使用者可根据自己的不同需要进行选择
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
`cmd/worker` 现在会同时启动：
- 下载任务 worker（`download.queue`）
- 活动统计 worker（`activity.queue`）

### 前端访问
打开 `static/index.html`，在页面中设置 API Base 为 `http://localhost:8000/api`。
当前新增页面：
- `static/pages/profile.html`：个人资料编辑 + 活动统计
- `static/pages/library.html`：收藏/最近访问/常用目录
- `static/pages/share-analytics.html`：分享访问日志与来源统计

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

## 分布式演进计划
- 已整合到本文附录“分布式演进路线”。

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

## 附录：AI Agent 集成计划

# AI Agent 集成计划（Go_Pan）

本文档给出在 Go_Pan 中接入 AI Agent 的可执行方案，覆盖目标、架构、里程碑、接口与安全要求，便于落地实施与迭代。

## 1. 目标与范围
目标是引入一个“可查询、可理解、可执行”的 AI 助手，服务于网盘核心流程。

### 目标
- 语义检索与问答：通过自然语言查找文件、分享、离线下载任务等。
- 任务执行：在权限允许时执行创建目录、分享链接、离线下载、重命名、移动等动作。
- 运维辅助：常见问题排查、配置说明、系统状态提示。

### 非目标（当前阶段不做）
- 自动化批量删除等高风险动作。
- 直接修改后端配置或执行系统命令。
- 大规模数据挖掘或行为推荐系统。

## 2. 使用场景清单
- 文件查询：例如“找上周上传的发票”“我昨天分享给张三的链接有哪些”。
- 内容问答：例如“这个项目里怎么做离线下载”“分享过期机制是什么”。
- 任务操作：例如“帮我新建一个 2025 报销 文件夹”“生成这个文件的分享链接 7 天有效”。
- 故障引导：例如“为什么下载失败”“Redis 过期事件没有触发怎么办”。

## 3. 方案概览（高层架构）
1. 前端新增 AI 助手入口（独立页面或侧边栏组件）。
2. 后端新增 AI 服务层，负责：
   - 对话编排（prompt、历史、工具调用）
   - 权限校验与审计记录
   - 检索增强（RAG）
3. 工具层将业务能力封装为“可调用工具”，供 Agent 使用。
4. 可选向量检索组件（Redis/外部向量库/内置索引）支撑文档与元数据检索。

## 4. 里程碑计划（建议）

### 阶段 0：需求与范围确认（1-2 天）
交付物：
- 明确目标用例与可执行动作清单
- 风险列表（高危操作、权限边界、费用预算）

### 阶段 1：最小可用 Agent（MVP，3-5 天）
能力：
- 基础对话
- 只读查询（文件列表、分享记录、下载任务）
交付物：
- `POST /api/ai/chat` 接口
- 基础提示词模板
- 审计日志与限流

### 阶段 2：可执行工具（5-10 天）
能力：
- 受控写操作：新建目录、生成分享、离线下载任务创建、重命名
- 强制二次确认（高风险操作）
交付物：
- 工具注册与路由
- 工具执行审计日志
- 权限映射表（用户权限 -> 工具许可）

### 阶段 3：检索增强与知识库（5-8 天）
能力：
- 基于项目文档和用户文件元数据的检索
- 指定范围检索（仅当前目录、仅分享记录）
交付物：
- 轻量向量索引或 Redis/外部向量库接入
- 文档/元数据同步与索引更新任务

### 阶段 4：体验与稳定性（持续）
能力：
- UI 体验优化、快捷操作按钮
- 结果可解释性与来源展示
- 成本与性能优化

## 5. 模块设计建议（与现有结构对齐）
建议新增目录（可按项目实际调整）：
- `internal/agent/`：对话编排、工具调度、模型调用
- `internal/agent/tools/`：业务工具封装（文件、分享、离线下载等）
- `router/ai.go`：AI 接口路由
- `model/ai.go`：请求/响应与审计模型
- `config/ai.go`：AI 相关配置项

## 6. API 设计草案
### 6.1 对话接口
`POST /api/ai/chat`
- 入参：`message`, `conversation_id`（可选）
- 返回：`reply`, `actions`（可选）、`sources`（可选）

### 6.2 工具执行接口（后端内部调用）
`POST /api/ai/tools/execute`
- 入参：`tool_name`, `args`
- 需权限校验与审计日志

### 6.3 审计与统计（可选）
`GET /api/ai/audit`
- 查询对话与工具执行记录

## 7. 安全与合规要求
- 权限强校验：所有工具调用都走现有鉴权体系。
- 风险操作二次确认：删除、移动、分享外链等操作需要确认。
- 限流与配额：对每用户/每 IP 限制请求频率。
- 审计与追踪：保存对话与工具执行日志，包含输入、输出、耗时、是否失败。
- 机密信息保护：脱敏处理敏感字段（邮箱、外链口令等）。

## 8. 检索增强（RAG）策略
检索对象：
- 文件元数据（名称、路径、标签、时间）
- 分享记录、下载任务记录
- 项目文档（`README.md`、FAQ）

检索流程（简版）：
1. 解析用户意图
2. 生成检索查询
3. 返回 topK 结果 + 片段
4. 将结果注入模型上下文

## 9. 前端改造建议
- `static/` 中新增 AI 页面或侧边助手组件
- 支持快捷操作按钮（“创建分享”“创建目录”）
- 展示来源与执行动作（透明化）
- 提供会话历史与撤销入口（如可用）

## 10. 测试与验收
测试重点：
- 工具权限校验（无权限时必须拒绝）
- 审计日志完整性
- RAG 检索准确性
- 对话异常与超时处理

验收标准：
- 常见查询命中率与响应时间达标
- 高风险操作必须二次确认
- 关键操作全程可追踪

## 11. 配置项建议
新增环境变量（示例）：
- `AI_PROVIDER`：模型供应商
- `AI_API_KEY`：调用密钥
- `AI_MODEL`：模型名称
- `AI_EMBEDDING_MODEL`：向量模型
- `AI_MAX_TOKENS`：最大输出
- `AI_TIMEOUT`：请求超时
- `AI_RATE_LIMIT`：速率限制

## 12. 下一步
请确认以下信息，以便进入实施：
1. 首批必须支持的具体场景（最多 3 个）
2. 允许的写操作范围
3. 预算与模型供应商偏好
4. 是否要集成本地模型或仅云服务



## 附录：分布式演进路线

# Go_Pan Distributed Roadmap

## 目标
- 在不重构主业务的前提下，先做横向扩展，突出分布式工程能力。
- 优先复用现有技术栈：MySQL + Redis + RabbitMQ + MinIO。
- 保持每个阶段都可上线、可测试、可写进简历。

## 当前状态（2026-02）
- 已上线：离线下载异步队列（RabbitMQ + worker）。
- 已上线：用户活动事件流与统计聚合。
  - 事件发布：业务 -> RabbitMQ `activity.exchange`
  - 事件消费：worker -> MySQL `user_activity_daily` + Redis `activity:daily:*`
  - 查询接口：`GET /api/user/activity/summary?days=N`
- 未主链路接入：
  - `internal/repo/sharding.go`（分库分表管理器）
  - `internal/storage/storage_cluster.go`（多存储节点与迁移）

## 分阶段计划
### Phase 1: 分库路由接入（低风险）
- 新增 `DB_SHARDING_ENABLED`、`DB_SHARD_COUNT` 配置。
- 先在“强 user_id 归属”的查询入口接入分库路由（文件列表、回收站列表、分享列表）。
- 保持默认关闭，灰度启用。
- 交付标准：
  - 开关关闭时与当前行为一致。
  - 开关开启且分片库存在时，读写走目标分片库。

### Phase 2: 表分片与数据迁移（中风险）
- 选 `user_file` 作为首个分表对象（按 user_id 哈希）。
- 增加影子表 + 迁移脚本 + 双写校验窗口。
- 引入分片一致性检查任务（定时巡检计数、抽样比对）。
- 交付标准：
  - 查询/写入延迟无明显退化。
  - 双写期间数据一致性可观测。

### Phase 3: 存储集群主链路（中风险）
- 把上传入口从单 `storage.Default` 切到 `StorageCluster.SelectNode()`。
- 启用副本策略和节点健康探测。
- 将迁移监控从“手工/测试”变为定时任务。
- 交付标准：
  - 单节点故障时上传可降级继续。
  - 节点容量超过阈值可触发自动迁移。

### Phase 4: 可观测与可靠性（持续）
- 指标：
  - MQ backlog、消费延迟、失败率
  - Redis 命中率、过期率
  - 分片库 QPS、慢查询、错误率
- 增加补偿任务：
  - 活动事件重放
  - 存储对象与数据库引用一致性修复

## 简历展示建议
- 写“做了什么”：
  - 设计并落地异步事件流水线，解耦主链路与统计计算。
  - 构建 MySQL + Redis 双层统计模型，兼顾实时与持久化。
- 写“指标结果”：
  - 主请求路径额外耗时基本为 0（仅异步投递）。
  - 统计查询优先 Redis，降低 MySQL 压力。
- 写“工程化”：
  - 幂等消费（event_id 去重）。
  - 失败回退策略（MQ 不可用时直写聚合）。


## 附录：内容横向扩展

# Content Expansion

## Goal
在不做深层分库分表改造的阶段，先把网盘功能从“上传/下载”扩到“用户内容体验 + 统计展示”。

## New Backend APIs

### User Profile
- `GET /api/user/me`
- `PUT /api/user/me`

可编辑字段：
- `nick_name`
- `email`
- `avatar_url`
- `bio`

### Favorites / Recent / Common Dirs
- `GET /api/user/favorites`
- `POST /api/user/favorites`
- `DELETE /api/user/favorites/:fileID`
- `GET /api/user/recent`
- `GET /api/user/common-dirs`

数据来源说明：
- 最近访问通过文件浏览/预览/下载链路写入 `user_recent`
- 常用目录由 `user_recent + user_file` 聚合得到

### Share Access Analytics
- `GET /api/share/access/logs`
- `GET /api/share/access/stats`

日志字段：
- `share_id`
- `owner_user_id`
- `file_id`
- `visitor_ip`
- `user_agent`
- `referer`
- `source`
- `accessed_at`

## New Frontend Pages
- `static/pages/profile.html`: 资料可编辑 + 活动统计
- `static/pages/library.html`: 收藏 / 最近访问 / 常用目录
- `static/pages/share-analytics.html`: 分享访问日志 + 来源统计

## Data Models
- `model/user_favorite.go`
- `model/user_recent.go`
- `model/share_access_log.go`

并已加入 `internal/repo/mysql.go` 自动迁移。



1.整体架构（单体 / 服务拆分）
2.文件存储和元数据是怎么分离的
3.上传 / 下载的大致流程
4.Redis / MQ 在系统里各自干什么
5.你认为这个系统的核心设计点是什么

整体架构是“单体 API + 异步 Worker”的两进程形态：main.go 启 HTTP API（Gin），cmd/worker/main.go 常驻消费队列做离线下载与活动统计；前端是静态页在 static/，数据层是 MySQL/Redis/MinIO/RabbitMQ。
整体架构（单体 / 服务拆分）
• 单体应用：HTTP API 全部在一个进程里（main.go + router/ + internal/handler/ + internal/service/）。
• 进程拆分：耗时/异步任务走独立 Worker（cmd/worker/main.go，internal/worker/）。
• 前后端分离：前端静态页直接访问 API（static/）。
• 存储层：MySQL 管元数据，MinIO 管对象，Redis 缓存/锁/TTL，RabbitMQ 做任务队列（README.md、internal/mq/）。
文件存储和元数据是怎么分离的
• 物理文件对象：model/file_object.go 记录 hash/bucket/object_name/size/ref_count；对应 MinIO 对象路径（internal/service/file_object.go 的 BuildObjectName）。
• 用户视角文件树：model/user_file.go 记录 user_id/parent_id/name/is_dir/object_id/size，体现目录结构和重命名、移动等操作。
• 关联关系：user_file.object_id -> file_object.id，多用户可复用同一对象（去重/秒传），靠 ref_count 管引用（IncreaseRefCount/DecreaseRefCount/RemoveObject）。
• 分片上传中间态：model/upload_session.go + model/file_chunk.go 记录会话与分片，分片对象临时存 MinIO chunks/<upload_id>/<index>。
上传 / 下载的大致流程
• 秒传（hash）：POST /api/file/upload/hash → service.FastUpload 查 file_object 是否存在且对象可用 → ref_count+1 → 创建 user_file。
• 分片上传：
i)upload/multipart/init → service.MultiPartFileInit 创建/复用 upload_session
ii)upload/multipart/chunk → service.UploadChunk 把分片写 MinIO + upsert file_chunk
iii)upload/multipart/complete → Redis 锁防并发合并（internal/handler/minio.go）→ service.CompleteFile Compose 合并 → 创建/更新 file_object 与 user_file → 清理分片/会话
• URL 上传：POST /api/file/upload/url → 校验 URL（SSRF/内网限制）→ DownloadByHTTP 写 MinIO → 建 file_object + user_file。
• 直连下载：POST /api/file/download/minio → 校验权限 → MinIO GetObject 流式输出（internal/handler/minio.go）。
• 预签名下载：POST /api/file/download/url → MinIO presigned URL（internal/service/minio.go）。
• 打包下载：POST /api/file/download/archive → 逐个对象读出写入 zip 流（internal/handler/file.go）。
• 离线下载：POST /api/file/download/offline 创建任务 → MQ 投递 → Worker 下载并落库/落对象，带重试与 DLQ（internal/task/、internal/worker/）。
Redis / MQ 在系统里各自干什么
• Redis
◦ 缓存：文件列表/用户信息/文件对象（utils/cache.go）。
◦ 锁：分片合并防并发（internal/handler/minio.go）。
◦ 分享过期：share:<id> TTL + keyspace 过期事件标记数据库状态（internal/repo/redis.go + internal/service/share_file.go）。
◦ 活动统计：事件去重 + 当日计数缓存（internal/activity/aggregate.go）。
◦ 注册激活：注册 token 暂存（internal/handler/register.go）。
• RabbitMQ
◦ 离线下载任务：task/retry/DLQ 三套交换机与队列（internal/mq/rabbitmq.go，internal/worker/download_worker.go）。
◦ 活动事件流：activity.queue 由 worker 统一落统计（internal/worker/activity_worker.go）。
我认为的核心设计点
• 对象层与用户视角分离：file_object + user_file 解耦，支持去重、秒传、共享、回收等能力扩展。
• 内容寻址 + 引用计数：基于 hash 复用对象、ref_count 管生命周期，是节省存储与实现秒传的关键。
• 异步化长任务：离线下载与活动统计从 API 剥离到 MQ/Worker，保证主链路轻量稳定。
• 缓存与一致性策略：Redis 作为缓存/TTL/锁，配合数据库落地，性能与正确性兼顾。

6.如果 CloudVault 的并发上传量突然增长 10 倍， 你认为系统最先出现瓶颈的地方会是哪里？为什么？

1)MinIO / 对象存储 I/O 与网络带宽
◦ 每个分片上传都同步 PutObject（service.UploadChunk），完成合并时还要 ComposeObject（service.CompleteFile）。
◦ 上传链路是“请求线程直写对象存储”，没有异步缓冲或排队，所以并发量一上来最先被打满的通常是对象存储的磁盘/网卡吞吐。
2)MySQL 元数据写入与索引冲突
◦ 分片上传每个 chunk 都要 file_chunk upsert（UploadChunk），合并后还要写 file_object 和 user_file。
◦ file_chunk 有 (upload_id, chunk_index) 唯一索引，user_file 也有 (user_id, parent_id, name, is_deleted) 复合唯一索引；高并发时锁竞争和写放大会明显。
3)API 节点自身的连接数与资源
◦ Gin 服务不做上传队列，所有上传请求都会占用连接和 goroutine；如果单机并发飙升，CPU/内存/FD 也可能成为次级瓶颈。
Redis 在上传链路里主要是合并锁，只有 multipart/complete 时才用；它通常不会是“最先”的瓶颈，除非大量并发“完成合并”同时发生。
简短结论：**最先卡住的通常是 MinIO 的吞吐，其次是 MySQL 的写压力与索引冲突，最后才是 API 进程资源。**如果你需要，我可以基于具体机器规格和配置给出更精确的排序。

7 不允许拆服务、不允许引入新基础设施（比如 Kafka、对象存储集群重构）， 只能在“现有技术栈”内优化上传性能。

1)减少分片数量 / 降低写放大
◦ 提升 chunk_size，甚至做“自适应分片”（小文件不分片，大文件大分片）。
◦ 结果：MinIO PutObject 次数减少，file_chunk 写入减少。
2)改用 MinIO Multipart Upload（仍是现有 MinIO）
◦ 目前是“分片对象 + ComposeObject”（service.CompleteFile），会产生额外对象与 I/O。
◦ 改成 MinIO 原生 multipart upload：每分片直接上传到同一 UploadID，完成时 CompleteMultipart，减少中间对象与合并成本。
3)分片状态从 MySQL 移到 Redis（现有 Redis）
◦ 分片上传过程用 Redis 记录完成情况（bitmap/set），Complete 时一次性落库。
◦ MySQL 只保存会话与最终文件对象，避免每个分片都写 DB。
4)流式上传，避免临时文件/重复 IO
◦ 现在 FormFile 会触发 multipart 解析，可能落临时文件。
◦ 改为 multipart.Reader 流式读，直接 PutObject，减少磁盘 IO 与内存压力。
5)并发与背压控制（仍在 API 进程内）
◦ 使用 Redis 或内存信号量限制每用户/每 IP 并发上传数，避免“洪峰”把 MinIO/DB 打死。
◦ Gin 层保持快失败 + 排队，保护后端。
6)MySQL 连接池与索引微调
◦ 按机器能力调大 SetMaxOpenConns/MaxIdleConns（internal/repo/mysql.go）。
◦ 确认 file_chunk 查询路径有合适索引（upload_id + status），避免合并时扫表。


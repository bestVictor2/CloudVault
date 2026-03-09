# CloudVault

CloudVault 是一个面向大文件传输和高并发场景的网盘后端项目，提供上传、下载、分享、回收站、离线下载、搜索、预览与 AI 助手能力。

## 功能完成度核查（2026-03-09）

| 能力 | 状态 | 说明 |
| --- | --- | --- |
| 文件上传（普通/秒传/分片） | 已完成 | 支持 Hash 秒传、分片上传、断点续传、分片状态回传 |
| 分片合并幂等控制 | 已完成 | `Redis` 分布式锁保护合并互斥，避免重复合并 |
| 文件下载（流式/直链/批量 ZIP） | 已完成 | 支持服务端流式下载、预签名 URL、ZIP 打包下载 |
| 分享与提取码 | 已完成 | 创建分享、提取码校验、过期失效、公开下载 |
| 回收站 | 已完成 | 支持列表、恢复、彻底删除，删除链路处理对象引用计数 |
| 离线下载任务系统 | 已完成 | `RabbitMQ` 入队、Worker 并发消费、失败重试、延迟重试、DLQ |
| 离线任务进度查询 | 已完成 | 任务状态查询 + 流式下载进度回写（非仅 0/100） |
| 搜索能力 | 已完成 | `Elasticsearch` 检索 + 索引同步 + `MySQL` 回退查询 |
| 预览能力 | 已完成 | 预览直链，支持可选视频转码预览 |
| Redis 缓存与失效策略 | 已完成 | 用户信息、文件对象、文件列表缓存 + 主动失效 |
| AI Agent（Function Calling） | 已完成 | 自然语言文件查询、下载链接生成、分享链接生成 |
| RAG 文档问答 | 已完成 | 新增 `/api/ai/rag`，检索用户文档片段后生成答案并返回引用来源 |
| AI 对话历史记录 | 已完成 | Redis 持久化用户历史；支持历史查询与清空，`AI_HISTORY_TTL` 控制过期时间 |

## 架构概览

- 对象存储与元数据解耦
  - 对象数据：`MinIO`
  - 业务元数据：`MySQL`（`file_object`、`user_file`、`upload_session`、`file_chunk` 等）
- 高并发一致性
  - 合并阶段：`Redis` 分布式锁
  - 去重机制：`Hash + RefCount`
- 异步任务
  - `RabbitMQ`：主队列 + 重试队列 + DLQ
  - Worker：并发控制 + 限速 + 重试策略
- 检索与 AI
  - 检索：`Elasticsearch`（失败/不一致时回退 `MySQL`）
  - AI Agent：函数调用工具
  - RAG：检索召回 -> 片段构建 -> 大模型生成

## 本次补齐内容

- 修复搜索一致性
  - 强化 ES 结果校验，索引延迟/脏索引场景自动回退 MySQL，保证查询正确性。
- 增强离线下载任务进度
  - 下载流中按阈值回写 `download_task.progress`，任务页面可看到持续进度变化。
- 新增 RAG 文档问答接口
  - `POST /api/ai/rag`
  - 返回答案、模型名、引用片段（文件 ID/名称/片段内容）。
- 新增 AI 历史记录能力
  - 历史自动持久化（Redis），支持服务端历史续聊（无需前端携带完整 history）。
  - 新增 `GET /api/ai/history`、`DELETE /api/ai/history`。
  - 新增配置项 `AI_HISTORY_TTL`（默认 `72h`）。

## 关键 API

- 认证
  - `POST /api/register`
  - `GET /api/activate`
  - `POST /api/login`
- 文件
  - `POST /api/file/list`
  - `POST /api/file/search`
  - `POST /api/file/upload/hash`
  - `POST /api/file/upload/multipart/init`
  - `POST /api/file/upload/multipart/chunk`
  - `POST /api/file/upload/multipart/complete`
  - `POST /api/file/download/minio`
  - `POST /api/file/download/url`
  - `POST /api/file/download/archive`
  - `GET /api/file/preview/:fileID`
- 离线下载
  - `POST /api/file/download/offline`
  - `GET /api/file/download/tasks`
- 回收站
  - `POST /api/recycle/list`
  - `POST /api/recycle/restore`
  - `POST /api/recycle/delete`
- 分享
  - `POST /api/share/create`
  - `GET /api/share/download/:shareID`
  - `GET /api/share/access/logs`
  - `GET /api/share/access/stats`
- AI
  - `POST /api/ai/ask`
  - `POST /api/ai/rag`
  - `GET /api/ai/history`
  - `DELETE /api/ai/history`

## AI 历史记录

- 服务端会在 `/api/ai/ask` 与 `/api/ai/rag` 成功返回后，自动写入当前用户历史记录。
- `GET /api/ai/history?limit=40`
  - 返回当前用户最近会话消息（`items`）。
- `DELETE /api/ai/history`
  - 清空当前用户历史记录。
- 相关配置：
  - `AI_HISTORY_LIMIT`：历史保留条数（默认 `20`，用于构造上下文与存储裁剪）。
  - `AI_HISTORY_TTL`：历史键过期时间（默认 `72h`，`0` 表示不过期）。

## RAG 接口示例

```http
POST /api/ai/rag
Content-Type: application/json
Authorization: Bearer <token>

{
  "question": "总结一下我最近上传的设计文档重点",
  "top_k": 5
}
```

返回示例（节选）：

```json
{
  "answer": "...",
  "model": "...",
  "references": [
    {
      "file_id": 12,
      "file_name": "design.md",
      "path": "/docs/design.md",
      "snippet": "..."
    }
  ]
}
```

## 运行要求

- Go 1.24+
- MySQL
- Redis
- MinIO
- RabbitMQ
- Elasticsearch（可选，关闭时自动走 MySQL 搜索）

## 启动

```powershell
go run .
```

启动 Worker：

```powershell
go run ./cmd/worker
```

## 测试

```powershell
go test ./...
```

## License

[MIT](LICENSE)

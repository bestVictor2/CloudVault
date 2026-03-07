# CloudVault AI Agent

CloudVault now supports tool-enabled AI Agent on:

- `POST /api/ai/ask` (requires JWT)

## Capabilities

The agent can call internal tools under current user scope:

- `list_files`: list files in a folder
- `search_files`: search files by keyword
- `list_recycle_files`: list recycle-bin items
- `get_preview_url`: generate preview URL for a file
- `get_download_url`: generate presigned download URL
- `create_share_link`: create share link for a file

All tool calls are user-isolated by `user_id` from JWT.

## Request Example

```json
{
  "question": "帮我找一下名字里有 report 的文件，并给最新那个生成下载链接",
  "history": []
}
```

## Response Example

```json
{
  "answer": "我已找到 3 个 report 文件，最新的是 report-2026.pdf。下载链接如下：...",
  "model": "openrouter/free",
  "tool_traces": [
    {
      "name": "search_files",
      "arguments": {
        "query": "report"
      }
    },
    {
      "name": "get_download_url",
      "arguments": {
        "file_id": 123
      }
    }
  ]
}
```

## Notes

- If provider does not support tool calling, service falls back to normal chat completion.
- Link TTL defaults to 10 minutes.
- `AI_API_BASE`, `AI_API_KEY`, `AI_MODEL` must be configured.

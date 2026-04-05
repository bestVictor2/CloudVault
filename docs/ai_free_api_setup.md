# CloudVault AI Free API Setup

This page explains how to make `static/pages/ai.html` call a real LLM API.

## Recommended Provider

- Website: https://openrouter.ai/
- API docs: https://openrouter.ai/docs/api-reference/overview
- Free models list: https://openrouter.ai/models?max_price=0

OpenRouter is OpenAI-compatible and works directly with this project.

## Backend Environment Variables (PowerShell)

Run these before `go run .`:

```powershell
$env:AI_API_BASE='https://openrouter.ai/api/v1'
$env:AI_API_KEY='your_openrouter_key'
$env:AI_MODEL='openrouter/free'

# optional
$env:AI_TIMEOUT='60s'
$env:AI_MAX_TOKENS='1024'
$env:AI_HISTORY_LIMIT='20'
$env:AI_X_TITLE='CloudVault'
$env:AI_HTTP_REFERER='http://localhost'

# optional: RAG embedding rerank
$env:AI_EMBEDDING_MODEL='text-embedding-3-small'
$env:AI_EMBEDDINGS_PATH='/v1/embeddings'
$env:AI_RAG_RERANK_ENABLED='true'
$env:AI_RAG_RECALL_TOP_K='20'
```

Notes:
- `AI_MODEL='openrouter/free'` is the easiest start.
- You can also use a concrete free model id with `:free` suffix.

## Start Server

```powershell
go run .
```

Default API base for frontend:
- `http://localhost:8000/api`

## Frontend Test

1. Open `static/pages/auth.html` and login.
2. Open `static/pages/ai.html`.
3. Ensure API Base is `http://localhost:8000/api`.
4. Send a message.

## Common Errors

- `AI API key not configured: set AI_API_KEY`
- `AI model not configured: set AI_MODEL`
- `AI request failed: ...` (usually invalid key/model, quota, or provider-side rate limit)

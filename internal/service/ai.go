package service

import (
	"CloudVault/config"
	"CloudVault/internal/dto"
	"CloudVault/model"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	aiAgentMaxRounds            = 4
	aiAgentMaxToolCallsPerRound = 4
	aiAgentFileRowsLimit        = 20
	aiAgentLinkTTL              = 10 * time.Minute
)

const aiAgentSystemInstruction = "你是 CloudVault 的 AI Agent。涉及用户文件状态、ID、链接、回收站、分享时，优先调用工具获取真实数据，禁止臆造文件信息或链接。对有副作用的操作（如创建分享）需先说明将执行的动作，再返回执行结果。"

type aiAPIMessage struct {
	Role       string       `json:"role"`
	Content    any          `json:"content,omitempty"`
	Name       string       `json:"name,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
	ToolCalls  []aiToolCall `json:"tool_calls,omitempty"`
}

type aiChatRequest struct {
	Model       string             `json:"model"`
	Messages    []aiAPIMessage     `json:"messages"`
	Tools       []aiToolDefinition `json:"tools,omitempty"`
	ToolChoice  any                `json:"tool_choice,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
}

type aiToolDefinition struct {
	Type     string             `json:"type"`
	Function aiToolFunctionSpec `json:"function"`
}

type aiToolFunctionSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type aiToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function aiToolFunctionCall `json:"function"`
}

type aiToolFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type aiChatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content   any          `json:"content"`
			ToolCalls []aiToolCall `json:"tool_calls"`
		} `json:"message"`
		Text string `json:"text"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error,omitempty"`
}

type aiRequestError struct {
	StatusCode int
	Message    string
}

func (e *aiRequestError) Error() string {
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = "AI request failed"
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("%s (http: %d)", msg, e.StatusCode)
	}
	return msg
}

// AskAI sends a chat request to the configured AI API and enables tool-based agent abilities.
func AskAI(ctx context.Context, userID uint64, req dto.AIAskRequest) (*dto.AIAskResponse, error) {
	if config.AppConfig.AIAPIBase == "" {
		return nil, errors.New("AI API not configured: set AI_API_BASE")
	}
	if config.AppConfig.AIAPIKey == "" {
		return nil, errors.New("AI API key not configured: set AI_API_KEY")
	}
	modelName := strings.TrimSpace(config.AppConfig.AIModel)
	if modelName == "" {
		return nil, errors.New("AI model not configured: set AI_MODEL")
	}
	if userID == 0 {
		return nil, errors.New("unauthorized")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	question := strings.TrimSpace(req.Question)
	if question == "" {
		return nil, errors.New("question required")
	}

	messages := buildAIConversationMessages(req, question)
	toolTraces := make([]dto.AIToolTrace, 0, aiAgentMaxRounds)

	activeModel := modelName
	for round := 0; round < aiAgentMaxRounds; round++ {
		parsed, err := requestAIChat(ctx, modelName, messages, true)
		if err != nil {
			if round == 0 && (isLikelyToolUnsupported(err) || isRetriableAIProviderError(err)) {
				return askAIWithoutTools(ctx, modelName, messages)
			}
			return nil, err
		}
		if parsed.Model != "" {
			activeModel = parsed.Model
		}
		if len(parsed.Choices) == 0 {
			return nil, errors.New("AI response empty")
		}
		choice := parsed.Choices[0]
		toolCalls := normalizeAIToolCalls(choice.Message.ToolCalls, round)
		if len(toolCalls) == 0 {
			toolCalls = synthesizeToolCallsFromText(choice.Message.Content, round)
		}
		if len(toolCalls) == 0 {
			answer := extractAIAnswer(choice.Message.Content, choice.Text)
			if answer == "" {
				return nil, errors.New("AI response empty")
			}
			return &dto.AIAskResponse{
				Answer:     answer,
				Model:      activeModel,
				ToolTraces: toolTraces,
			}, nil
		}

		messages = append(messages, aiAPIMessage{
			Role:      "assistant",
			Content:   choice.Message.Content,
			ToolCalls: toolCalls,
		})

		if len(toolCalls) > aiAgentMaxToolCallsPerRound {
			toolCalls = toolCalls[:aiAgentMaxToolCallsPerRound]
		}
		for _, call := range toolCalls {
			result, arguments, err := executeAIToolCall(ctx, userID, call)
			trace := dto.AIToolTrace{
				Name:      call.Function.Name,
				Arguments: arguments,
			}

			toolPayload := map[string]any{
				"ok": true,
			}
			if err != nil {
				trace.Error = err.Error()
				toolPayload["ok"] = false
				toolPayload["error"] = err.Error()
			} else {
				trace.Result = result
				toolPayload["result"] = result
			}
			toolTraces = append(toolTraces, trace)

			messages = append(messages, aiAPIMessage{
				Role:       "tool",
				Name:       call.Function.Name,
				ToolCallID: call.ID,
				Content:    mustMarshalJSON(toolPayload),
			})
		}
	}

	answer := buildAIToolFallbackAnswer(toolTraces)
	if answer == "" {
		answer = "已执行多轮工具调用，但未生成最终答案，请重试。"
	}
	return &dto.AIAskResponse{
		Answer:     answer,
		Model:      activeModel,
		ToolTraces: toolTraces,
	}, nil
}

func askAIWithoutTools(ctx context.Context, modelName string, messages []aiAPIMessage) (*dto.AIAskResponse, error) {
	parsed, err := requestAIChat(ctx, modelName, messages, false)
	if err != nil {
		return nil, err
	}
	if len(parsed.Choices) == 0 {
		return nil, errors.New("AI response empty")
	}
	answer := extractAIAnswer(parsed.Choices[0].Message.Content, parsed.Choices[0].Text)
	if answer == "" {
		return nil, errors.New("AI response empty")
	}
	if parsed.Model != "" {
		modelName = parsed.Model
	}
	return &dto.AIAskResponse{
		Answer: answer,
		Model:  modelName,
	}, nil
}

func requestAIChat(ctx context.Context, modelName string, messages []aiAPIMessage, enableTools bool) (*aiChatResponse, error) {
	payload := aiChatRequest{
		Model:       modelName,
		Messages:    messages,
		Temperature: 0.3,
		MaxTokens:   config.AppConfig.AIMaxTokens,
	}
	if enableTools {
		payload.Tools = buildAIToolDefinitions()
		payload.ToolChoice = "auto"
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout: config.AppConfig.AIRequestTimeout,
	}
	url := resolveAIChatCompletionsURL(config.AppConfig.AIAPIBase, config.AppConfig.AIChatCompletionsPath)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+config.AppConfig.AIAPIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	if referer := strings.TrimSpace(config.AppConfig.AIHTTPReferer); referer != "" {
		httpReq.Header.Set("HTTP-Referer", referer)
	}
	if title := strings.TrimSpace(config.AppConfig.AIXTitle); title != "" {
		httpReq.Header.Set("X-Title", title)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(respBody)) == 0 {
		return nil, errors.New("AI response empty")
	}

	var parsed aiChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		if resp.StatusCode >= 400 {
			return nil, &aiRequestError{
				StatusCode: resp.StatusCode,
				Message:    compactErrorText(respBody),
			}
		}
		return nil, fmt.Errorf("invalid AI response: %w", err)
	}
	if resp.StatusCode >= 400 {
		if parsed.Error != nil && parsed.Error.Message != "" {
			return nil, &aiRequestError{
				StatusCode: resp.StatusCode,
				Message:    composeAIError(parsed.Error.Message, parsed.Error.Code),
			}
		}
		return nil, &aiRequestError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("AI request failed: %s", resp.Status),
		}
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, errors.New(composeAIError(parsed.Error.Message, parsed.Error.Code))
	}
	return &parsed, nil
}

func buildAIConversationMessages(req dto.AIAskRequest, question string) []aiAPIMessage {
	history := trimHistory(req.History, config.AppConfig.AIHistoryLimit)
	messages := make([]aiAPIMessage, 0, len(history)+3)

	systemPrompt := strings.TrimSpace(config.AppConfig.AISystemPrompt)
	if systemPrompt == "" {
		systemPrompt = "You are a concise CloudVault assistant. Answer in Chinese for Chinese questions."
	}
	systemPrompt = strings.TrimSpace(systemPrompt + "\n" + aiAgentSystemInstruction)
	if systemPrompt != "" {
		messages = append(messages, aiAPIMessage{Role: "system", Content: systemPrompt})
	}

	lastRole := ""
	lastContent := ""
	for _, msg := range history {
		role := normalizeAIRole(msg.Role)
		content := strings.TrimSpace(msg.Content)
		if role == "" || content == "" {
			continue
		}
		messages = append(messages, aiAPIMessage{
			Role:    role,
			Content: content,
		})
		lastRole = role
		lastContent = content
	}

	if !(lastRole == "user" && lastContent == question) {
		messages = append(messages, aiAPIMessage{Role: "user", Content: question})
	}
	return messages
}

func buildAIToolDefinitions() []aiToolDefinition {
	return []aiToolDefinition{
		{
			Type: "function",
			Function: aiToolFunctionSpec{
				Name:        "list_files",
				Description: "列出当前用户在指定目录下的文件列表",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"parent_id": map[string]any{
							"type":        "integer",
							"description": "目录ID，0表示根目录",
						},
						"page": map[string]any{
							"type":    "integer",
							"default": 1,
						},
						"page_size": map[string]any{
							"type":    "integer",
							"default": 20,
						},
						"order_by": map[string]any{
							"type":        "string",
							"description": "created_at/updated_at/name/size",
						},
						"order_desc": map[string]any{
							"type":    "boolean",
							"default": true,
						},
					},
				},
			},
		},
		{
			Type: "function",
			Function: aiToolFunctionSpec{
				Name:        "search_files",
				Description: "按关键字搜索当前用户文件",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "搜索词",
						},
						"parent_id": map[string]any{
							"type":        "integer",
							"description": "可选，目录ID，0为全局根级范围",
						},
						"page": map[string]any{
							"type":    "integer",
							"default": 1,
						},
						"page_size": map[string]any{
							"type":    "integer",
							"default": 20,
						},
					},
					"required": []string{"query"},
				},
			},
		},
		{
			Type: "function",
			Function: aiToolFunctionSpec{
				Name:        "list_recycle_files",
				Description: "查看当前用户回收站文件",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"limit": map[string]any{
							"type":    "integer",
							"default": 20,
						},
					},
				},
			},
		},
		{
			Type: "function",
			Function: aiToolFunctionSpec{
				Name:        "get_preview_url",
				Description: "为指定文件生成预览链接",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"file_id": map[string]any{
							"type":        "integer",
							"description": "文件ID",
						},
					},
					"required": []string{"file_id"},
				},
			},
		},
		{
			Type: "function",
			Function: aiToolFunctionSpec{
				Name:        "get_download_url",
				Description: "为指定文件生成预签名下载链接",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"file_id": map[string]any{
							"type":        "integer",
							"description": "文件ID",
						},
					},
					"required": []string{"file_id"},
				},
			},
		},
		{
			Type: "function",
			Function: aiToolFunctionSpec{
				Name:        "create_share_link",
				Description: "为指定文件创建分享链接",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"file_id": map[string]any{
							"type":        "integer",
							"description": "文件ID",
						},
						"expire_days": map[string]any{
							"type":        "integer",
							"description": "有效天数，0表示不过期",
							"default":     7,
						},
						"need_code": map[string]any{
							"type":        "boolean",
							"description": "是否需要提取码",
							"default":     false,
						},
					},
					"required": []string{"file_id"},
				},
			},
		},
	}
}

func executeAIToolCall(ctx context.Context, userID uint64, call aiToolCall) (any, map[string]any, error) {
	arguments, err := parseAIToolArguments(call.Function.Arguments)
	if err != nil {
		return nil, nil, err
	}

	switch strings.TrimSpace(call.Function.Name) {
	case "list_files":
		result, toolErr := aiToolListFiles(userID, arguments)
		return result, arguments, toolErr
	case "search_files":
		result, toolErr := aiToolSearchFiles(userID, arguments)
		return result, arguments, toolErr
	case "list_recycle_files":
		result, toolErr := aiToolListRecycleFiles(userID, arguments)
		return result, arguments, toolErr
	case "get_preview_url":
		result, toolErr := aiToolGetPreviewURL(ctx, userID, arguments)
		return result, arguments, toolErr
	case "get_download_url":
		result, toolErr := aiToolGetDownloadURL(ctx, userID, arguments)
		return result, arguments, toolErr
	case "create_share_link":
		result, toolErr := aiToolCreateShareLink(userID, arguments)
		return result, arguments, toolErr
	default:
		return nil, arguments, fmt.Errorf("unknown tool: %s", strings.TrimSpace(call.Function.Name))
	}
}

func aiToolListFiles(userID uint64, args map[string]any) (map[string]any, error) {
	parentID, err := getOptionalUint64Arg(args, "parent_id")
	if err != nil {
		return nil, err
	}
	page := clampInt(getOptionalIntArg(args, "page", 1), 1, 1000)
	pageSize := clampInt(getOptionalIntArg(args, "page_size", 20), 1, 50)
	orderBy := strings.TrimSpace(getOptionalStringArg(args, "order_by", "updated_at"))
	orderDesc := getOptionalBoolArg(args, "order_desc", true)

	req := &dto.FileListRequest{
		Page:      page,
		PageSize:  pageSize,
		OrderBy:   orderBy,
		OrderDesc: orderDesc,
	}
	if parentID > 0 {
		req.ParentID = &parentID
	}

	files, total, err := GetFileList(userID, req)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"total":      total,
		"page":       page,
		"page_size":  pageSize,
		"parent_id":  parentID,
		"truncated":  len(files) > aiAgentFileRowsLimit,
		"files":      summarizeUserFiles(files),
		"tool_notes": "仅返回用户本人可见文件",
	}, nil
}

func aiToolSearchFiles(userID uint64, args map[string]any) (map[string]any, error) {
	query := strings.TrimSpace(getOptionalStringArg(args, "query", ""))
	if query == "" {
		return nil, errors.New("query required")
	}
	parentID, err := getOptionalUint64Arg(args, "parent_id")
	if err != nil {
		return nil, err
	}
	page := clampInt(getOptionalIntArg(args, "page", 1), 1, 1000)
	pageSize := clampInt(getOptionalIntArg(args, "page_size", 20), 1, 50)

	req := &dto.FileSearchRequest{
		Query:     query,
		Page:      page,
		PageSize:  pageSize,
		OrderBy:   "updated_at",
		OrderDesc: true,
	}
	if parentID > 0 {
		req.ParentID = &parentID
	}

	files, total, err := SearchFiles(userID, req)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"query":     query,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		"parent_id": parentID,
		"truncated": len(files) > aiAgentFileRowsLimit,
		"files":     summarizeUserFiles(files),
	}, nil
}

func aiToolListRecycleFiles(userID uint64, args map[string]any) (map[string]any, error) {
	limit := clampInt(getOptionalIntArg(args, "limit", 20), 1, 50)
	files, err := ListRecycleFiles(uint(userID))
	if err != nil {
		return nil, err
	}
	if len(files) > limit {
		files = files[:limit]
	}
	return map[string]any{
		"limit": limit,
		"files": summarizeRecycleFiles(files),
	}, nil
}

func aiToolGetPreviewURL(ctx context.Context, userID uint64, args map[string]any) (map[string]any, error) {
	fileID, err := getRequiredUint64Arg(args, "file_id")
	if err != nil {
		return nil, err
	}
	if !CheckFileOwner(userID, fileID) {
		return nil, errors.New("file not found")
	}
	url, err := GetPreviewURL(ctx, userID, fileID, aiAgentLinkTTL)
	if err != nil {
		return nil, err
	}
	_ = RecordRecentAccess(userID, fileID, "preview")
	return map[string]any{
		"file_id":            fileID,
		"preview_url":        url,
		"expires_in_seconds": int(aiAgentLinkTTL.Seconds()),
	}, nil
}

func aiToolGetDownloadURL(ctx context.Context, userID uint64, args map[string]any) (map[string]any, error) {
	fileID, err := getRequiredUint64Arg(args, "file_id")
	if err != nil {
		return nil, err
	}
	if !CheckFileOwner(userID, fileID) {
		return nil, errors.New("file not found")
	}
	userFile, err := GetUserFileById(fileID)
	if err != nil || userFile.ObjectID == nil {
		return nil, errors.New("file not found")
	}
	obj, err := GetFileObjectById(*userFile.ObjectID)
	if err != nil {
		return nil, errors.New("file not found")
	}
	url, err := GetDownloadURL(ctx, obj.BucketName, obj.ObjectName, userFile.Name, aiAgentLinkTTL)
	if err != nil {
		return nil, err
	}
	_ = RecordRecentAccess(userID, fileID, "download_url")
	return map[string]any{
		"file_id":            fileID,
		"name":               userFile.Name,
		"size":               userFile.Size,
		"download_url":       url,
		"expires_in_seconds": int(aiAgentLinkTTL.Seconds()),
	}, nil
}

func aiToolCreateShareLink(userID uint64, args map[string]any) (map[string]any, error) {
	fileID, err := getRequiredUint64Arg(args, "file_id")
	if err != nil {
		return nil, err
	}
	expireDays := clampInt(getOptionalIntArg(args, "expire_days", 7), 0, 365)
	needCode := getOptionalBoolArg(args, "need_code", false)

	share, err := CreateShare(userID, fileID, expireDays, needCode)
	if err != nil {
		return nil, err
	}

	out := map[string]any{
		"file_id":    fileID,
		"share_id":   share.ShareID,
		"share_path": "/api/share/download/" + share.ShareID,
		"share_url":  buildShareURL(share.ShareID),
		"need_code":  share.NeedCode,
	}
	if strings.TrimSpace(share.ExtractCode) != "" {
		out["extract_code"] = strings.TrimSpace(share.ExtractCode)
	}
	if share.ExpireAt != nil {
		out["expire_at"] = share.ExpireAt.Format(time.RFC3339)
	}
	return out, nil
}

func buildShareURL(shareID string) string {
	base := strings.TrimSpace(config.AppConfig.AIHTTPReferer)
	if strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://") {
		return strings.TrimRight(base, "/") + "/api/share/download/" + strings.TrimSpace(shareID)
	}
	return "/api/share/download/" + strings.TrimSpace(shareID)
}

func summarizeUserFiles(files []model.UserFile) []map[string]any {
	limit := len(files)
	if limit > aiAgentFileRowsLimit {
		limit = aiAgentFileRowsLimit
	}
	out := make([]map[string]any, 0, limit)
	for i := 0; i < limit; i++ {
		file := files[i]
		parentID := uint64(0)
		if file.ParentID != nil {
			parentID = *file.ParentID
		}
		out = append(out, map[string]any{
			"file_id":    file.ID,
			"name":       file.Name,
			"is_dir":     file.IsDir,
			"size":       file.Size,
			"parent_id":  parentID,
			"updated_at": file.UpdatedAt.Format(time.RFC3339),
		})
	}
	return out
}

func summarizeRecycleFiles(files []model.UserFile) []map[string]any {
	out := make([]map[string]any, 0, len(files))
	for _, file := range files {
		parentID := uint64(0)
		if file.ParentID != nil {
			parentID = *file.ParentID
		}
		deletedAt := ""
		if file.DeletedAt.Valid {
			deletedAt = file.DeletedAt.Time.Format(time.RFC3339)
		}
		out = append(out, map[string]any{
			"file_id":    file.ID,
			"name":       file.Name,
			"is_dir":     file.IsDir,
			"size":       file.Size,
			"parent_id":  parentID,
			"deleted_at": deletedAt,
		})
	}
	return out
}

func normalizeAIToolCalls(calls []aiToolCall, round int) []aiToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]aiToolCall, 0, len(calls))
	for i, call := range calls {
		call.Function.Name = strings.TrimSpace(call.Function.Name)
		if call.Function.Name == "" {
			continue
		}
		call.ID = strings.TrimSpace(call.ID)
		if call.ID == "" {
			call.ID = fmt.Sprintf("tool_call_%d_%d", round+1, i+1)
		}
		out = append(out, call)
	}
	return out
}

func synthesizeToolCallsFromText(content any, round int) []aiToolCall {
	text := strings.TrimSpace(extractContentText(content))
	if text == "" {
		return nil
	}
	if !strings.Contains(strings.ToLower(text), "<tool_call") && !strings.Contains(strings.ToLower(text), "<function") {
		return nil
	}

	// Compatible with models that emit pseudo tool tags:
	// <tool_call><function=list_files>{...}</function></tool_call>
	re := regexp.MustCompile(`(?is)<function\s*=\s*([a-zA-Z0-9_]+)\s*>(.*?)</function>`)
	matches := re.FindAllStringSubmatch(text, aiAgentMaxToolCallsPerRound)
	if len(matches) == 0 {
		return nil
	}

	out := make([]aiToolCall, 0, len(matches))
	for i, match := range matches {
		name := strings.TrimSpace(match[1])
		if name == "" {
			continue
		}
		args := strings.TrimSpace(match[2])
		if args == "" {
			args = "{}"
		}
		out = append(out, aiToolCall{
			ID:   fmt.Sprintf("tool_call_text_%d_%d", round+1, i+1),
			Type: "function",
			Function: aiToolFunctionCall{
				Name:      name,
				Arguments: args,
			},
		})
	}
	return out
}

func parseAIToolArguments(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}, nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()

	var args map[string]any
	if err := decoder.Decode(&args); err != nil {
		return nil, fmt.Errorf("invalid tool arguments: %w", err)
	}
	if args == nil {
		args = map[string]any{}
	}
	return args, nil
}

func getRequiredUint64Arg(args map[string]any, key string) (uint64, error) {
	value, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("%s required", key)
	}
	out, err := toUint64(value)
	if err != nil {
		return 0, fmt.Errorf("%s invalid", key)
	}
	if out == 0 {
		return 0, fmt.Errorf("%s required", key)
	}
	return out, nil
}

func getOptionalUint64Arg(args map[string]any, key string) (uint64, error) {
	value, ok := args[key]
	if !ok {
		return 0, nil
	}
	out, err := toUint64(value)
	if err != nil {
		return 0, fmt.Errorf("%s invalid", key)
	}
	return out, nil
}

func getOptionalIntArg(args map[string]any, key string, defaultValue int) int {
	value, ok := args[key]
	if !ok {
		return defaultValue
	}
	out, err := toInt(value)
	if err != nil {
		return defaultValue
	}
	return out
}

func getOptionalBoolArg(args map[string]any, key string, defaultValue bool) bool {
	value, ok := args[key]
	if !ok {
		return defaultValue
	}
	out, err := toBool(value)
	if err != nil {
		return defaultValue
	}
	return out
}

func getOptionalStringArg(args map[string]any, key string, defaultValue string) string {
	value, ok := args[key]
	if !ok {
		return defaultValue
	}
	text, ok := value.(string)
	if !ok {
		return defaultValue
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return defaultValue
	}
	return text
}

func toUint64(value any) (uint64, error) {
	switch v := value.(type) {
	case json.Number:
		num, err := v.Int64()
		if err != nil || num < 0 {
			return 0, errors.New("invalid number")
		}
		return uint64(num), nil
	case float64:
		if v < 0 {
			return 0, errors.New("invalid number")
		}
		return uint64(v), nil
	case float32:
		if v < 0 {
			return 0, errors.New("invalid number")
		}
		return uint64(v), nil
	case int:
		if v < 0 {
			return 0, errors.New("invalid number")
		}
		return uint64(v), nil
	case int64:
		if v < 0 {
			return 0, errors.New("invalid number")
		}
		return uint64(v), nil
	case uint64:
		return v, nil
	case uint:
		return uint64(v), nil
	case string:
		var out json.Number = json.Number(strings.TrimSpace(v))
		num, err := out.Int64()
		if err != nil || num < 0 {
			return 0, errors.New("invalid number")
		}
		return uint64(num), nil
	default:
		return 0, errors.New("unsupported type")
	}
}

func toInt(value any) (int, error) {
	switch v := value.(type) {
	case json.Number:
		num, err := v.Int64()
		if err != nil {
			return 0, err
		}
		return int(num), nil
	case float64:
		return int(v), nil
	case float32:
		return int(v), nil
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case uint64:
		return int(v), nil
	case uint:
		return int(v), nil
	case string:
		var out json.Number = json.Number(strings.TrimSpace(v))
		num, err := out.Int64()
		if err != nil {
			return 0, err
		}
		return int(num), nil
	default:
		return 0, errors.New("unsupported type")
	}
}

func toBool(value any) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "y", "on":
			return true, nil
		case "0", "false", "no", "n", "off":
			return false, nil
		default:
			return false, errors.New("invalid bool")
		}
	default:
		return false, errors.New("unsupported type")
	}
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func mustMarshalJSON(v any) string {
	body, err := json.Marshal(v)
	if err != nil {
		return `{"ok":false,"error":"marshal failed"}`
	}
	return string(body)
}

func buildAIToolFallbackAnswer(traces []dto.AIToolTrace) string {
	if len(traces) == 0 {
		return ""
	}
	last := traces[len(traces)-1]
	if last.Error != "" {
		return fmt.Sprintf("工具 `%s` 执行失败：%s", last.Name, last.Error)
	}
	return "我已完成所需数据查询，可继续告诉我下一步动作（如“生成下载链接”或“按关键词再筛选”）。"
}

func normalizeAIRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "system", "user", "assistant":
		return strings.ToLower(strings.TrimSpace(role))
	default:
		return ""
	}
}

func trimHistory(history []dto.AIMessage, limit int) []dto.AIMessage {
	if limit <= 0 || len(history) <= limit {
		return history
	}
	return history[len(history)-limit:]
}

func resolveAIChatCompletionsURL(apiBase, chatPath string) string {
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	path := strings.TrimSpace(chatPath)
	if path == "" {
		if strings.HasSuffix(base, "/v1") {
			return base + "/chat/completions"
		}
		return base + "/v1/chat/completions"
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if strings.HasSuffix(base, "/v1") && strings.HasPrefix(path, "/v1/") {
		path = strings.TrimPrefix(path, "/v1")
	}
	return base + path
}

func extractAIAnswer(content any, fallback string) string {
	if answer := strings.TrimSpace(extractContentText(content)); answer != "" {
		return answer
	}
	return strings.TrimSpace(fallback)
}

func extractContentText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			text := strings.TrimSpace(extractContentText(item))
			if text == "" {
				continue
			}
			parts = append(parts, text)
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			return text
		}
		if inputText, ok := v["input_text"].(string); ok {
			return inputText
		}
		if contentText, ok := v["content"].(string); ok {
			return contentText
		}
		if nested, ok := v["content"]; ok {
			return extractContentText(nested)
		}
	}
	return ""
}

func compactErrorText(body []byte) string {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return "unknown error"
	}
	const maxLen = 240
	if len(text) > maxLen {
		return text[:maxLen] + "..."
	}
	return text
}

func composeAIError(message string, code any) string {
	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = "AI request failed"
	}
	if code == nil {
		return msg
	}
	switch v := code.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return msg
		}
		return fmt.Sprintf("%s (code: %s)", msg, strings.TrimSpace(v))
	default:
		return fmt.Sprintf("%s (code: %v)", msg, v)
	}
}

func isLikelyToolUnsupported(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	if !strings.Contains(text, "tool") {
		return false
	}
	return strings.Contains(text, "unsupported") ||
		strings.Contains(text, "unknown") ||
		strings.Contains(text, "not support") ||
		strings.Contains(text, "invalid_request_error")
}

func isRetriableAIProviderError(err error) bool {
	if err == nil {
		return false
	}
	var reqErr *aiRequestError
	if errors.As(err, &reqErr) {
		return reqErr.StatusCode == http.StatusTooManyRequests || reqErr.StatusCode >= http.StatusInternalServerError
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "code: 500") ||
		strings.Contains(text, "http: 500") ||
		strings.Contains(text, "http: 502") ||
		strings.Contains(text, "http: 503") ||
		strings.Contains(text, "http: 504") ||
		strings.Contains(text, "code: 429")
}

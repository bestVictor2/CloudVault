package dto

// AIMessage represents a chat message for the AI assistant.
type AIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AIAskRequest is the request payload for the AI assistant.
type AIAskRequest struct {
	Question string      `json:"question" binding:"required"`
	History  []AIMessage `json:"history"`
}

// AIAskResponse is the response payload for the AI assistant.
type AIAskResponse struct {
	Answer     string        `json:"answer"`
	Model      string        `json:"model,omitempty"`
	ToolTraces []AIToolTrace `json:"tool_traces,omitempty"`
}

// AIToolTrace is one executed agent tool call for observability.
type AIToolTrace struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Result    any            `json:"result,omitempty"`
	Error     string         `json:"error,omitempty"`
}

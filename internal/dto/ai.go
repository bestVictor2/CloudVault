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

// AIRAGAskRequest is the request payload for RAG-based QA.
type AIRAGAskRequest struct {
	Question string      `json:"question" binding:"required"`
	TopK     int         `json:"top_k"`
	History  []AIMessage `json:"history"`
}

// AIRAGReference is one retrieved knowledge snippet for answer grounding.
type AIRAGReference struct {
	FileID   uint64  `json:"file_id"`
	FileName string  `json:"file_name"`
	Path     string  `json:"path,omitempty"`
	Snippet  string  `json:"snippet"`
	Score    float64 `json:"score,omitempty"`
}

// AIRAGAskResponse is the response payload for RAG-based QA.
type AIRAGAskResponse struct {
	Answer     string           `json:"answer"`
	Model      string           `json:"model,omitempty"`
	References []AIRAGReference `json:"references,omitempty"`
}

// AIToolTrace is one executed agent tool call for observability.
type AIToolTrace struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Result    any            `json:"result,omitempty"`
	Error     string         `json:"error,omitempty"`
}

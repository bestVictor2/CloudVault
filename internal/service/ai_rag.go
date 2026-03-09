package service

import (
	"CloudVault/config"
	"CloudVault/internal/dto"
	"CloudVault/internal/repo"
	"CloudVault/model"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	aiRAGDefaultTopK      = 5
	aiRAGMaxTopK          = 8
	aiRAGSnippetMaxChars  = 480
	aiRAGContextMaxRefs   = 8
	aiRAGNoContextMessage = "No relevant document snippets were found. Please refine your query or ensure files are indexed."
)

type esRAGSearchResponse struct {
	Hits struct {
		Hits []struct {
			ID     string  `json:"_id"`
			Score  float64 `json:"_score"`
			Source struct {
				FileID  uint64 `json:"file_id"`
				Name    string `json:"name"`
				Path    string `json:"path"`
				Content string `json:"content"`
			} `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

// AskAIRAG performs retrieval-augmented QA based on user files.
func AskAIRAG(ctx context.Context, userID uint64, req dto.AIRAGAskRequest) (*dto.AIRAGAskResponse, error) {
	if userID == 0 {
		return nil, errors.New("unauthorized")
	}
	question := strings.TrimSpace(req.Question)
	if question == "" {
		return nil, errors.New("question required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	topK := req.TopK
	if topK <= 0 {
		topK = aiRAGDefaultTopK
	}
	if topK > aiRAGMaxTopK {
		topK = aiRAGMaxTopK
	}
	history := resolveAIConversationHistory(ctx, userID, req.History)

	references, err := RetrieveRAGReferences(ctx, userID, question, topK)
	if err != nil {
		return nil, err
	}
	if len(references) == 0 {
		persistAIConversation(ctx, userID, history, question, aiRAGNoContextMessage)
		return &dto.AIRAGAskResponse{
			Answer:     aiRAGNoContextMessage,
			References: references,
		}, nil
	}

	modelName, err := getAIModelName()
	if err != nil {
		return nil, err
	}
	messages := buildAIConversationMessages(
		dto.AIAskRequest{
			Question: question,
			History:  history,
		},
		question,
	)
	messages = append([]aiAPIMessage{
		{
			Role:    "system",
			Content: buildRAGContextPrompt(references),
		},
	}, messages...)

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
	persistAIConversation(ctx, userID, history, question, answer)
	if parsed.Model != "" {
		modelName = parsed.Model
	}
	return &dto.AIRAGAskResponse{
		Answer:     answer,
		Model:      modelName,
		References: references,
	}, nil
}

// RetrieveRAGReferences retrieves ranked snippets from user files.
func RetrieveRAGReferences(
	ctx context.Context,
	userID uint64,
	query string,
	topK int,
) ([]dto.AIRAGReference, error) {
	if topK <= 0 {
		topK = aiRAGDefaultTopK
	}
	if topK > aiRAGMaxTopK {
		topK = aiRAGMaxTopK
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("query required")
	}

	if isESSearchEnabled() {
		refs, err := retrieveRAGReferencesByES(ctx, userID, query, topK)
		if err == nil && len(refs) > 0 {
			return refs, nil
		}
	}
	return retrieveRAGReferencesByDB(ctx, userID, query, topK)
}

func retrieveRAGReferencesByES(
	ctx context.Context,
	userID uint64,
	query string,
	topK int,
) ([]dto.AIRAGReference, error) {
	if !isESSearchEnabled() {
		return nil, errors.New("es disabled")
	}
	if err := ensureESIndex(ctx); err != nil {
		return nil, err
	}

	payload := map[string]any{
		"size":             topK,
		"track_total_hits": false,
		"_source":          []string{"file_id", "name", "path", "content"},
		"query": map[string]any{
			"bool": map[string]any{
				"must": []map[string]any{
					{
						"multi_match": map[string]any{
							"query":  query,
							"fields": []string{"content^6", "name^3", "path^2"},
						},
					},
				},
				"filter": []map[string]any{
					{"term": map[string]any{"user_id": userID}},
					{"term": map[string]any{"is_deleted": false}},
				},
			},
		},
		"sort": []map[string]any{
			{"_score": map[string]string{"order": "desc"}},
			{"updated_at": map[string]string{"order": "desc"}},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	status, respBody, err := doESRequest(
		ctx,
		http.MethodPost,
		"/"+url.PathEscape(config.AppConfig.ESIndex)+"/_search",
		body,
	)
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, fmt.Errorf("es rag search failed: status=%d body=%s", status, compactErrorText(respBody))
	}

	var parsed esRAGSearchResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}
	refs := make([]dto.AIRAGReference, 0, topK)
	for _, hit := range parsed.Hits.Hits {
		fileID := hit.Source.FileID
		if fileID == 0 {
			parsedID, parseErr := strconv.ParseUint(strings.TrimSpace(hit.ID), 10, 64)
			if parseErr != nil {
				continue
			}
			fileID = parsedID
		}
		file, err := loadRAGUserFile(userID, fileID)
		if err != nil {
			continue
		}
		snippet := buildRAGSnippet(hit.Source.Content, query, aiRAGSnippetMaxChars)
		if snippet == "" {
			snippet = loadRAGSnippetFromObject(ctx, file, query)
		}
		if snippet == "" {
			continue
		}
		refs = append(refs, dto.AIRAGReference{
			FileID:   file.ID,
			FileName: file.Name,
			Path:     hit.Source.Path,
			Snippet:  snippet,
			Score:    hit.Score,
		})
		if len(refs) >= topK {
			break
		}
	}
	return refs, nil
}

func retrieveRAGReferencesByDB(
	ctx context.Context,
	userID uint64,
	query string,
	topK int,
) ([]dto.AIRAGReference, error) {
	req := &dto.FileSearchRequest{
		Query:     query,
		Page:      1,
		PageSize:  topK * 3,
		OrderBy:   "updated_at",
		OrderDesc: true,
	}
	files, _, err := searchFilesByDB(userID, req)
	if err != nil {
		return nil, err
	}
	refs := make([]dto.AIRAGReference, 0, topK)
	for _, file := range files {
		if file.IsDir || file.ObjectID == nil {
			continue
		}
		snippet := loadRAGSnippetFromObject(ctx, &file, query)
		if snippet == "" {
			continue
		}
		refs = append(refs, dto.AIRAGReference{
			FileID:   file.ID,
			FileName: file.Name,
			Snippet:  snippet,
		})
		if len(refs) >= topK {
			break
		}
	}
	return refs, nil
}

func loadRAGUserFile(userID uint64, fileID uint64) (*model.UserFile, error) {
	var file model.UserFile
	err := repo.Db.
		Where("id = ? AND user_id = ? AND is_deleted = 0 AND is_dir = 0", fileID, userID).
		First(&file).Error
	if err != nil {
		return nil, err
	}
	return &file, nil
}

func loadRAGSnippetFromObject(ctx context.Context, file *model.UserFile, query string) string {
	if file == nil || file.ObjectID == nil {
		return ""
	}
	obj, err := GetFileObjectById(*file.ObjectID)
	if err != nil {
		return ""
	}
	content := extractTextContentForSearch(ctx, file.Name, obj)
	return buildRAGSnippet(content, query, aiRAGSnippetMaxChars)
}

func buildRAGContextPrompt(refs []dto.AIRAGReference) string {
	if len(refs) > aiRAGContextMaxRefs {
		refs = refs[:aiRAGContextMaxRefs]
	}
	var builder strings.Builder
	builder.WriteString("You are the CloudVault document QA assistant.\n")
	builder.WriteString("Use only the provided snippets as evidence. If evidence is insufficient, state that clearly.\n")
	builder.WriteString("Snippets:\n")
	for i, ref := range refs {
		path := strings.TrimSpace(ref.Path)
		if path == "" {
			path = "/"
		}
		builder.WriteString(fmt.Sprintf("[%d] file_id=%d name=%s path=%s\n", i+1, ref.FileID, ref.FileName, path))
		builder.WriteString(ref.Snippet)
		builder.WriteString("\n")
	}
	builder.WriteString("Answer requirements: concise, accurate, include citation ids like [1][3], and do not hallucinate.")
	return builder.String()
}

func buildRAGSnippet(content, query string, maxChars int) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	if maxChars <= 0 {
		maxChars = aiRAGSnippetMaxChars
	}
	content = strings.Join(strings.Fields(content), " ")
	runes := []rune(content)
	if len(runes) <= maxChars {
		return content
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return string(runes[:maxChars]) + "..."
	}
	lowerContent := []rune(strings.ToLower(content))
	lowerQuery := []rune(strings.ToLower(query))
	pos := findSubRuneIndex(lowerContent, lowerQuery)
	if pos < 0 {
		return string(runes[:maxChars]) + "..."
	}

	start := pos - maxChars/3
	if start < 0 {
		start = 0
	}
	end := start + maxChars
	if end > len(runes) {
		end = len(runes)
		start = end - maxChars
		if start < 0 {
			start = 0
		}
	}

	snippet := string(runes[start:end])
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(runes) {
		snippet += "..."
	}
	return snippet
}

func findSubRuneIndex(text, sub []rune) int {
	if len(sub) == 0 || len(text) < len(sub) {
		return -1
	}
	last := len(text) - len(sub)
	for i := 0; i <= last; i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			if text[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func getAIModelName() (string, error) {
	if strings.TrimSpace(config.AppConfig.AIAPIBase) == "" {
		return "", errors.New("AI API not configured: set AI_API_BASE")
	}
	if strings.TrimSpace(config.AppConfig.AIAPIKey) == "" {
		return "", errors.New("AI API key not configured: set AI_API_KEY")
	}
	modelName := strings.TrimSpace(config.AppConfig.AIModel)
	if modelName == "" {
		return "", errors.New("AI model not configured: set AI_MODEL")
	}
	return modelName, nil
}

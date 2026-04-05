package service

import (
	"CloudVault/config"
	"CloudVault/internal/dto"
	"CloudVault/internal/repo"
	"CloudVault/model"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	aiRAGDefaultTopK       = 5
	aiRAGMaxTopK           = 8
	aiRAGDefaultRecallTopK = 20
	aiRAGMaxRecallTopK     = 50
	aiRAGSnippetMaxChars   = 480
	aiRAGContextMaxRefs    = 8
	aiRAGNoContextMessage  = "No relevant document snippets were found. Please refine your query or ensure files are indexed."
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

type aiEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type aiEmbeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error,omitempty"`
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
	recallTopK := resolveRAGRecallTopK(topK)

	if isESSearchEnabled() {
		if err := EnsureUserFilesSearchIndexed(ctx, userID); err != nil {
			log.Printf("es rag backfill skipped: %v", err)
		}
		refs, err := retrieveRAGReferencesByES(ctx, userID, query, recallTopK)
		if err == nil && len(refs) > 0 {
			return rerankAndLimitRAGReferences(ctx, query, refs, topK), nil
		}
	}

	refs, err := retrieveRAGReferencesByDB(ctx, userID, query, recallTopK)
	if err != nil {
		return nil, err
	}
	return rerankAndLimitRAGReferences(ctx, query, refs, topK), nil
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
					{"term": map[string]any{"is_dir": false}},
					{"exists": map[string]any{"field": "content"}},
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
		PageSize:  topK,
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
			Path:     buildUserFilePath(&file),
			Snippet:  snippet,
		})
		if len(refs) >= topK {
			break
		}
	}
	return refs, nil
}

func resolveRAGRecallTopK(topK int) int {
	recallTopK := config.AppConfig.AIRAGRecallTopK
	if recallTopK <= 0 {
		recallTopK = maxInt(aiRAGDefaultRecallTopK, topK*4)
	}
	if recallTopK < topK {
		recallTopK = topK
	}
	if recallTopK > aiRAGMaxRecallTopK {
		recallTopK = aiRAGMaxRecallTopK
	}
	return recallTopK
}

func maxInt(a, b int) int {
	if a >= b {
		return a
	}
	return b
}

func rerankAndLimitRAGReferences(
	ctx context.Context,
	query string,
	refs []dto.AIRAGReference,
	topK int,
) []dto.AIRAGReference {
	if len(refs) == 0 {
		return refs
	}
	if !isRAGEmbeddingRerankEnabled() {
		return limitRAGReferences(refs, topK)
	}
	reranked, err := rerankRAGReferencesByEmbedding(ctx, query, refs, topK)
	if err != nil {
		log.Printf("rag embedding rerank skipped: %v", err)
		return limitRAGReferences(refs, topK)
	}
	return reranked
}

func isRAGEmbeddingRerankEnabled() bool {
	if !config.AppConfig.AIRAGRerankEnabled {
		return false
	}
	return strings.TrimSpace(config.AppConfig.AIAPIBase) != "" &&
		strings.TrimSpace(config.AppConfig.AIAPIKey) != "" &&
		strings.TrimSpace(config.AppConfig.AIEmbeddingModel) != ""
}

func limitRAGReferences(refs []dto.AIRAGReference, topK int) []dto.AIRAGReference {
	if topK <= 0 || len(refs) <= topK {
		return refs
	}
	return refs[:topK]
}

func rerankRAGReferencesByEmbedding(
	ctx context.Context,
	query string,
	refs []dto.AIRAGReference,
	topK int,
) ([]dto.AIRAGReference, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return limitRAGReferences(refs, topK), nil
	}
	inputs := make([]string, 0, len(refs)+1)
	inputs = append(inputs, query)
	for _, ref := range refs {
		inputs = append(inputs, strings.TrimSpace(ref.Snippet))
	}
	embeddings, err := requestAIEmbeddings(ctx, inputs)
	if err != nil {
		return nil, err
	}
	if len(embeddings) != len(inputs) {
		return nil, fmt.Errorf("embedding length mismatch: got=%d want=%d", len(embeddings), len(inputs))
	}

	type ragScoredRef struct {
		ref   dto.AIRAGReference
		score float64
		index int
	}
	queryEmbedding := embeddings[0]
	scored := make([]ragScoredRef, 0, len(refs))
	for i, ref := range refs {
		score := cosineSimilarity(queryEmbedding, embeddings[i+1])
		ref.Score = score
		scored = append(scored, ragScoredRef{
			ref:   ref,
			score: score,
			index: i,
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].index < scored[j].index
		}
		return scored[i].score > scored[j].score
	})

	if topK <= 0 || topK > len(scored) {
		topK = len(scored)
	}
	out := make([]dto.AIRAGReference, 0, topK)
	for i := 0; i < topK; i++ {
		out = append(out, scored[i].ref)
	}
	return out, nil
}

func requestAIEmbeddings(ctx context.Context, inputs []string) ([][]float64, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload := aiEmbeddingRequest{
		Model: strings.TrimSpace(config.AppConfig.AIEmbeddingModel),
		Input: inputs,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		resolveAIEmbeddingsURL(config.AppConfig.AIAPIBase, config.AppConfig.AIEmbeddingsPath),
		bytes.NewReader(body),
	)
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

	timeout := config.AppConfig.AIRequestTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}
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
		return nil, errors.New("AI embedding response empty")
	}
	var parsed aiEmbeddingResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		if resp.StatusCode >= 400 {
			return nil, &aiRequestError{
				StatusCode: resp.StatusCode,
				Message:    compactErrorText(respBody),
			}
		}
		return nil, fmt.Errorf("invalid AI embedding response: %w", err)
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
			Message:    fmt.Sprintf("AI embedding request failed: %s", resp.Status),
		}
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, errors.New(composeAIError(parsed.Error.Message, parsed.Error.Code))
	}
	if len(parsed.Data) == 0 {
		return nil, errors.New("AI embedding response empty")
	}

	out := make([][]float64, len(inputs))
	for i, item := range parsed.Data {
		idx := item.Index
		if idx < 0 || idx >= len(inputs) {
			idx = i
		}
		if idx < 0 || idx >= len(inputs) || len(item.Embedding) == 0 {
			continue
		}
		out[idx] = item.Embedding
	}
	for i := range out {
		if len(out[i]) == 0 {
			return nil, fmt.Errorf("embedding missing at index=%d", i)
		}
	}
	return out, nil
}

func resolveAIEmbeddingsURL(apiBase, embeddingsPath string) string {
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	path := strings.TrimSpace(embeddingsPath)
	if path == "" {
		if strings.HasSuffix(base, "/v1") {
			return base + "/embeddings"
		}
		return base + "/v1/embeddings"
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

func cosineSimilarity(a, b []float64) float64 {
	maxLen := len(a)
	if len(b) < maxLen {
		maxLen = len(b)
	}
	if maxLen == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := 0; i < maxLen; i++ {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	score := dot / (math.Sqrt(normA) * math.Sqrt(normB))
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return 0
	}
	return score
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

package service

import (
	"CloudVault/config"
	"CloudVault/internal/dto"
	"CloudVault/internal/repo"
	"CloudVault/internal/storage"
	"CloudVault/utils"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	downloadTicketKeyPrefix = "download:ticket:"
	previewTicketKeyPrefix  = "preview:ticket:"
	popChallengeTTL         = 2 * time.Minute
	popChallengeKeyPrefix   = "pop:challenge:"
)

type downloadTicketState struct {
	UserID    uint64    `json:"user_id"`
	FileID    uint64    `json:"file_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type previewTicketState struct {
	UserID    uint64    `json:"user_id"`
	FileID    uint64    `json:"file_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type popChallengeState struct {
	UserID uint64 `json:"user_id"`
	Hash   string `json:"hash"`
	Size   int64  `json:"size"`
	Nonce  string `json:"nonce"`
}

func buildDownloadTicketKey(token string) string {
	return downloadTicketKeyPrefix + token
}

func buildPreviewTicketKey(token string) string {
	return previewTicketKeyPrefix + token
}

func buildPoPChallengeKey(id string) string {
	return popChallengeKeyPrefix + id
}

// CreateDownloadTicket issues a download ticket bound to a user and file.
func CreateDownloadTicket(ctx context.Context, userID, fileID uint64, ttl time.Duration) (string, error) {
	if repo.Redis == nil {
		return "", errors.New("redis not initialized")
	}
	if userID == 0 || fileID == 0 {
		return "", errors.New("invalid download ticket scope")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ttl <= 0 {
		ttl = config.AppConfig.DownloadTicketTTL
	}
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	token := utils.GetToken()
	state := downloadTicketState{
		UserID:    userID,
		FileID:    fileID,
		ExpiresAt: time.Now().Add(ttl),
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	if err := repo.Redis.Set(ctx, buildDownloadTicketKey(token), payload, ttl).Err(); err != nil {
		return "", err
	}
	return token, nil
}

// VerifyDownloadTicket validates a download ticket.
func VerifyDownloadTicket(ctx context.Context, token string) (*downloadTicketState, error) {
	if repo.Redis == nil {
		return nil, errors.New("redis not initialized")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("download ticket missing")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	raw, err := repo.Redis.Get(ctx, buildDownloadTicketKey(token)).Bytes()
	if err != nil {
		return nil, errors.New("download ticket invalid or expired")
	}
	var state downloadTicketState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	if !state.ExpiresAt.IsZero() && time.Now().After(state.ExpiresAt) {
		return nil, errors.New("download ticket expired")
	}
	return &state, nil
}

// CreatePreviewTicket issues a preview ticket bound to a user and file.
func CreatePreviewTicket(ctx context.Context, userID, fileID uint64, ttl time.Duration) (string, error) {
	if repo.Redis == nil {
		return "", errors.New("redis not initialized")
	}
	if userID == 0 || fileID == 0 {
		return "", errors.New("invalid preview ticket scope")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ttl <= 0 {
		ttl = config.AppConfig.DownloadTicketTTL
	}
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	token := utils.GetToken()
	state := previewTicketState{
		UserID:    userID,
		FileID:    fileID,
		ExpiresAt: time.Now().Add(ttl),
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	if err := repo.Redis.Set(ctx, buildPreviewTicketKey(token), payload, ttl).Err(); err != nil {
		return "", err
	}
	return token, nil
}

// VerifyPreviewTicket validates a preview ticket.
func VerifyPreviewTicket(ctx context.Context, token string) (*previewTicketState, error) {
	if repo.Redis == nil {
		return nil, errors.New("redis not initialized")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("preview ticket missing")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	raw, err := repo.Redis.Get(ctx, buildPreviewTicketKey(token)).Bytes()
	if err != nil {
		return nil, errors.New("preview ticket invalid or expired")
	}
	var state previewTicketState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	if !state.ExpiresAt.IsZero() && time.Now().After(state.ExpiresAt) {
		return nil, errors.New("preview ticket expired")
	}
	return &state, nil
}

func CreatePoPChallenge(ctx context.Context, userID uint64, hash string, size int64) (*dto.PoPChallenge, error) {
	if repo.Redis == nil {
		return nil, errors.New("redis not initialized")
	}
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, errors.New("hash missing")
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return nil, err
	}
	nonce := hex.EncodeToString(nonceBytes)
	challengeID := utils.GetToken()
	state := popChallengeState{
		UserID: userID,
		Hash:   hash,
		Size:   size,
		Nonce:  nonce,
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	if err := repo.Redis.Set(ctx, buildPoPChallengeKey(challengeID), payload, popChallengeTTL).Err(); err != nil {
		return nil, err
	}
	return &dto.PoPChallenge{
		ChallengeID: challengeID,
		Nonce:       nonce,
		Algo:        "SHA-256",
	}, nil
}

func loadPoPChallenge(ctx context.Context, challengeID string) (*popChallengeState, error) {
	if repo.Redis == nil {
		return nil, errors.New("redis not initialized")
	}
	if strings.TrimSpace(challengeID) == "" {
		return nil, errors.New("challenge_id missing")
	}
	raw, err := repo.Redis.Get(ctx, buildPoPChallengeKey(challengeID)).Bytes()
	if err != nil {
		return nil, err
	}
	var state popChallengeState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func deletePoPChallenge(ctx context.Context, challengeID string) {
	if repo.Redis == nil || strings.TrimSpace(challengeID) == "" {
		return
	}
	_ = repo.Redis.Del(ctx, buildPoPChallengeKey(challengeID)).Err()
}

func ComputeObjectNonceHash(ctx context.Context, bucket, object, nonceHex string) (string, int64, error) {
	if storage.Default == nil {
		return "", 0, fmt.Errorf("storage not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	nonceHex = strings.TrimSpace(nonceHex)
	if nonceHex == "" {
		return "", 0, errors.New("nonce missing")
	}
	nonce, err := hex.DecodeString(nonceHex)
	if err != nil {
		return "", 0, err
	}
	reader, _, err := storage.Default.GetObject(ctx, bucket, object)
	if err != nil {
		return "", 0, err
	}
	defer reader.Close()
	hasher := sha256.New()
	if _, err := hasher.Write(nonce); err != nil {
		return "", 0, err
	}
	readBytes, err := io.Copy(hasher, reader)
	if err != nil {
		return "", readBytes, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), readBytes, nil
}

func VerifyPoPChallenge(
	ctx context.Context,
	userID uint64,
	challengeID string,
	proofHash string,
) (bool, string, *popChallengeState, error) {
	state, err := loadPoPChallenge(ctx, challengeID)
	if err != nil {
		return false, "challenge_missing", nil, nil
	}
	if state.UserID != userID {
		deletePoPChallenge(ctx, challengeID)
		return false, "challenge_forbidden", state, nil
	}
	proofHash = strings.ToLower(strings.TrimSpace(proofHash))
	if proofHash == "" {
		deletePoPChallenge(ctx, challengeID)
		return false, "proof_missing", state, nil
	}
	obj, err := GetFileObjectByHash(state.Hash)
	if err != nil {
		deletePoPChallenge(ctx, challengeID)
		return false, "hash_not_found", state, nil
	}
	available, err := isFileObjectAvailable(ctx, obj)
	if err != nil {
		deletePoPChallenge(ctx, challengeID)
		return false, "", state, err
	}
	if !available {
		deletePoPChallenge(ctx, challengeID)
		return false, "object_missing", state, nil
	}
	actualHash, _, err := ComputeObjectNonceHash(ctx, obj.BucketName, obj.ObjectName, state.Nonce)
	if err != nil {
		deletePoPChallenge(ctx, challengeID)
		return false, "", state, err
	}
	deletePoPChallenge(ctx, challengeID)
	if strings.ToLower(actualHash) != proofHash {
		return false, "proof_mismatch", state, nil
	}
	return true, "", state, nil
}

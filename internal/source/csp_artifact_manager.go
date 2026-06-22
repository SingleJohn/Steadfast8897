package source

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyms/internal/repository"
)

const cspArtifactMaxBytes = 64 << 20

type CSPArtifactManager struct {
	repo        *repository.SourceRepository
	client      *http.Client
	artifactDir string
}

type cspArtifactDownload struct {
	Artifact repository.SourceRuntimeArtifact
	Body     []byte
}

func NewCSPArtifactManager(repo *repository.SourceRepository, client *http.Client, dataDir string) *CSPArtifactManager {
	if client == nil {
		client = http.DefaultClient
	}
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "data"
	}
	return &CSPArtifactManager{
		repo:        repo,
		client:      client,
		artifactDir: filepath.Join(dataDir, "source-runtime", "csp", "artifacts"),
	}
}

func (m *CSPArtifactManager) Fetch(ctx context.Context, req CSPRuntimeRequest) (CSPRuntimeArtifact, error) {
	req = normalizeCSPRuntimeRequest(req)
	baseURL, err := url.Parse(req.ConfigBaseURL)
	if err != nil {
		return CSPRuntimeArtifact{}, fmt.Errorf("解析 configBaseUrl 失败: %w", err)
	}
	spiderRef, hashKind, hashValue, err := parseCSPSpiderRef(req.Spider, req.MD5)
	if err != nil {
		return CSPRuntimeArtifact{}, err
	}
	spiderURL, err := resolveDRPYURL(baseURL, spiderRef)
	if err != nil {
		return CSPRuntimeArtifact{}, err
	}
	download, err := m.fetch(ctx, req, spiderURL, hashKind, hashValue)
	if err != nil {
		return CSPRuntimeArtifact{}, err
	}
	return cspRuntimeArtifactFromRepo(download.Artifact), nil
}

func (m *CSPArtifactManager) fetch(ctx context.Context, req CSPRuntimeRequest, rawURL, hashKind, hashValue string) (cspArtifactDownload, error) {
	if err := ValidateOutboundURL(ctx, rawURL); err != nil {
		return cspArtifactDownload{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return cspArtifactDownload{}, err
	}
	httpReq.Header.Set("User-Agent", "FYMS-CSP-Runtime/1.0")
	resp, err := m.client.Do(httpReq)
	if err != nil {
		return cspArtifactDownload{}, fmt.Errorf("下载 csp_dex_jar artifact 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return cspArtifactDownload{}, fmt.Errorf("下载 csp_dex_jar artifact 返回异常状态: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, cspArtifactMaxBytes+1))
	if err != nil {
		return cspArtifactDownload{}, err
	}
	if len(body) > cspArtifactMaxBytes {
		return cspArtifactDownload{}, fmt.Errorf("csp_dex_jar artifact 超过大小上限")
	}
	md5sum := md5.Sum(body)
	sha := sha256.Sum256(body)
	md5Text := hex.EncodeToString(md5sum[:])
	shaText := hex.EncodeToString(sha[:])
	if err := verifyCSPArtifactHash(hashKind, hashValue, md5Text, shaText); err != nil {
		return cspArtifactDownload{}, err
	}
	localPath, err := m.save(rawURL, body, shaText)
	if err != nil {
		return cspArtifactDownload{}, err
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	var contentTypePtr *string
	if contentType != "" {
		contentTypePtr = &contentType
	}
	relative := relativeArtifactPath(req.ConfigBaseURL, rawURL)
	trust := "unverified"
	if strings.TrimSpace(hashValue) != "" {
		trust = "verified"
	}
	raw := jsonBytes(map[string]any{
		"providerKeyHash": URLHash(req.ProviderKey),
		"api":             req.API,
		"hashKind":        hashKind,
		"hashProvided":    strings.TrimSpace(hashValue) != "",
		"fetchedAt":       time.Now().UTC().Format(time.RFC3339),
	}, "{}")
	upsert := repository.SourceRuntimeArtifactUpsert{
		ProviderID:   req.ProviderID,
		SourceType:   "tvbox",
		ArtifactKind: "csp_dex_jar",
		Name:         sanitizeArtifactName(filepath.Base(urlPath(rawURL))),
		SourceURL:    rawURL,
		BaseURL:      stringPtrOrNil(req.ConfigBaseURL),
		RelativePath: relative,
		LocalPath:    localPath,
		MD5:          md5Text,
		SHA256:       shaText,
		ByteSize:     int64(len(body)),
		ContentType:  contentTypePtr,
		TrustStatus:  trust,
		Status:       "active",
		Raw:          raw,
	}
	if upsert.Name == "" || upsert.Name == "." {
		upsert.Name = "csp-spider.jar"
	}
	if m.repo == nil {
		return cspArtifactDownload{
			Artifact: repository.SourceRuntimeArtifact{
				ProviderID:   req.ProviderID,
				SourceType:   "tvbox",
				ArtifactKind: upsert.ArtifactKind,
				Name:         upsert.Name,
				SourceURL:    rawURL,
				BaseURL:      upsert.BaseURL,
				RelativePath: upsert.RelativePath,
				LocalPath:    localPath,
				MD5:          md5Text,
				SHA256:       shaText,
				ByteSize:     int64(len(body)),
				ContentType:  contentTypePtr,
				TrustStatus:  trust,
				Status:       "active",
				Raw:          raw,
			},
			Body: body,
		}, nil
	}
	artifact, err := m.repo.UpsertRuntimeArtifact(ctx, upsert)
	if err != nil {
		return cspArtifactDownload{}, err
	}
	return cspArtifactDownload{Artifact: *artifact, Body: body}, nil
}

func (m *CSPArtifactManager) save(rawURL string, body []byte, shaText string) (string, error) {
	if err := os.MkdirAll(m.artifactDir, 0755); err != nil {
		return "", err
	}
	name := sanitizeArtifactName("csp-" + filepath.Base(urlPath(rawURL)))
	if name == "csp-" || name == "csp-." {
		name = "csp-spider.jar"
	}
	path := filepath.Join(m.artifactDir, shaText[:16]+"-"+name)
	if err := os.WriteFile(path, body, 0644); err != nil {
		return "", err
	}
	return path, nil
}

func parseCSPSpiderRef(rawSpider, fallbackMD5 string) (string, string, string, error) {
	parts := strings.Split(strings.TrimSpace(rawSpider), ";")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "", "", "", fmt.Errorf("spider 路径为空")
	}
	ref := strings.TrimSpace(parts[0])
	hashKind := ""
	hashValue := ""
	if len(parts) >= 3 {
		hashKind = strings.ToLower(strings.TrimSpace(parts[1]))
		hashValue = strings.TrimSpace(parts[2])
	}
	if hashValue == "" && strings.TrimSpace(fallbackMD5) != "" {
		hashKind = "md5"
		hashValue = strings.TrimSpace(fallbackMD5)
	}
	return ref, hashKind, strings.ToLower(hashValue), nil
}

func verifyCSPArtifactHash(kind, expected, md5Text, shaText string) error {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if expected == "" {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "md5", "":
		if md5Text != expected {
			return fmt.Errorf("csp_dex_jar md5 校验失败: got %s", md5Text)
		}
	case "sha256", "sha":
		if shaText != expected {
			return fmt.Errorf("csp_dex_jar sha256 校验失败: got %s", shaText)
		}
	default:
		return fmt.Errorf("不支持的 spider hash 类型: %s", kind)
	}
	return nil
}

func cspRuntimeArtifactFromRepo(in repository.SourceRuntimeArtifact) CSPRuntimeArtifact {
	return CSPRuntimeArtifact{
		ID:           in.ID,
		Kind:         in.ArtifactKind,
		URL:          in.SourceURL,
		Path:         in.LocalPath,
		Bytes:        in.ByteSize,
		MD5:          in.MD5,
		SHA256:       in.SHA256,
		TrustStatus:  in.TrustStatus,
		ContentType:  in.ContentType,
		RelativePath: in.RelativePath,
	}
}

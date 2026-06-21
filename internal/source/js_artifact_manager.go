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

const jsArtifactMaxBytes = 2 << 20

type JSArtifactManager struct {
	repo        *repository.SourceRepository
	client      *http.Client
	artifactDir string
}

func NewJSArtifactManager(repo *repository.SourceRepository, client *http.Client, dataDir string) *JSArtifactManager {
	if client == nil {
		client = http.DefaultClient
	}
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "data"
	}
	return &JSArtifactManager{
		repo:        repo,
		client:      client,
		artifactDir: filepath.Join(dataDir, "source-runtime", "js"),
	}
}

func (m *JSArtifactManager) FetchPair(ctx context.Context, req JSRuntimeRequest) ([]JSRuntimeArtifact, map[string][]byte, error) {
	req = normalizeJSRuntimeRequest(req)
	baseURL, err := url.Parse(req.ConfigBaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("解析 configBaseUrl 失败: %w", err)
	}
	engineURL, err := resolveDRPYURL(baseURL, req.Engine)
	if err != nil {
		return nil, nil, err
	}
	ruleURL, err := resolveDRPYURL(baseURL, req.Rule)
	if err != nil {
		return nil, nil, err
	}
	engine, err := m.fetch(ctx, req, "drpy_engine", engineURL)
	if err != nil {
		return nil, nil, err
	}
	rule, err := m.fetch(ctx, req, "drpy_rule", ruleURL)
	if err != nil {
		return nil, nil, err
	}
	artifacts := []JSRuntimeArtifact{
		jsRuntimeArtifactFromRepo(engine.Artifact),
		jsRuntimeArtifactFromRepo(rule.Artifact),
	}
	bodies := map[string][]byte{
		"engine": engine.Body,
		"rule":   rule.Body,
	}
	return artifacts, bodies, nil
}

func (m *JSArtifactManager) fetch(ctx context.Context, req JSRuntimeRequest, kind, rawURL string) (jsArtifactDownload, error) {
	if err := ValidateOutboundURL(ctx, rawURL); err != nil {
		return jsArtifactDownload{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return jsArtifactDownload{}, err
	}
	httpReq.Header.Set("User-Agent", "FYMS-DRPY-Runtime/1.0")
	resp, err := m.client.Do(httpReq)
	if err != nil {
		return jsArtifactDownload{}, fmt.Errorf("下载 %s artifact 失败: %w", kind, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return jsArtifactDownload{}, fmt.Errorf("下载 %s artifact 返回异常状态: %d", kind, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, jsArtifactMaxBytes+1))
	if err != nil {
		return jsArtifactDownload{}, err
	}
	if len(body) > jsArtifactMaxBytes {
		return jsArtifactDownload{}, fmt.Errorf("%s artifact 超过大小上限", kind)
	}
	localPath, md5Text, shaText, err := m.save(kind, rawURL, body)
	if err != nil {
		return jsArtifactDownload{}, err
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	var contentTypePtr *string
	if contentType != "" {
		contentTypePtr = &contentType
	}
	relative := relativeArtifactPath(req.ConfigBaseURL, rawURL)
	raw := jsonBytes(map[string]any{
		"providerKey": req.ProviderKey,
		"fetchedAt":   time.Now().UTC().Format(time.RFC3339),
	}, "{}")
	upsert := repository.SourceRuntimeArtifactUpsert{
		ProviderID:   req.ProviderID,
		SourceType:   "tvbox",
		ArtifactKind: kind,
		Name:         sanitizeArtifactName(filepath.Base(urlPath(rawURL))),
		SourceURL:    rawURL,
		BaseURL:      stringPtrOrNil(req.ConfigBaseURL),
		RelativePath: relative,
		LocalPath:    localPath,
		MD5:          md5Text,
		SHA256:       shaText,
		ByteSize:     int64(len(body)),
		ContentType:  contentTypePtr,
		TrustStatus:  "unverified",
		Status:       "active",
		Raw:          raw,
	}
	if upsert.Name == "" || upsert.Name == "." {
		upsert.Name = kind + ".js"
	}
	if m.repo == nil {
		return jsArtifactDownload{
			Artifact: repository.SourceRuntimeArtifact{
				ProviderID:   req.ProviderID,
				SourceType:   "tvbox",
				ArtifactKind: kind,
				Name:         upsert.Name,
				SourceURL:    rawURL,
				BaseURL:      upsert.BaseURL,
				RelativePath: upsert.RelativePath,
				LocalPath:    localPath,
				MD5:          md5Text,
				SHA256:       shaText,
				ByteSize:     int64(len(body)),
				ContentType:  contentTypePtr,
				TrustStatus:  "unverified",
				Status:       "active",
				Raw:          raw,
			},
			Body: body,
		}, nil
	}
	artifact, err := m.repo.UpsertRuntimeArtifact(ctx, upsert)
	if err != nil {
		return jsArtifactDownload{}, err
	}
	return jsArtifactDownload{Artifact: *artifact, Body: body}, nil
}

func (m *JSArtifactManager) save(kind, rawURL string, body []byte) (string, string, string, error) {
	if err := os.MkdirAll(m.artifactDir, 0755); err != nil {
		return "", "", "", err
	}
	sha := sha256.Sum256(body)
	md5sum := md5.Sum(body)
	shaText := hex.EncodeToString(sha[:])
	md5Text := hex.EncodeToString(md5sum[:])
	name := sanitizeArtifactName(kind + "-" + filepath.Base(urlPath(rawURL)))
	if name == kind+"-" || name == kind+"-." {
		name = kind + ".js"
	}
	path := filepath.Join(m.artifactDir, shaText[:16]+"-"+name)
	if err := os.WriteFile(path, body, 0644); err != nil {
		return "", "", "", err
	}
	return path, md5Text, shaText, nil
}

func normalizeJSRuntimeRequest(req JSRuntimeRequest) JSRuntimeRequest {
	if strings.TrimSpace(req.ConfigBaseURL) == "" {
		req.ConfigBaseURL = defaultDRPYBaseURL
	}
	if strings.TrimSpace(req.Engine) == "" {
		req.Engine = defaultDRPYEngine
	}
	if strings.TrimSpace(req.Rule) == "" {
		req.Rule = defaultDRPYRule
	}
	if strings.TrimSpace(req.Method) == "" {
		req.Method = JSRuntimeMethodInit
	}
	req.Method = strings.ToLower(strings.TrimSpace(req.Method))
	if req.Args == nil {
		req.Args = map[string]any{}
	}
	return req
}

func relativeArtifactPath(baseRaw, artifactRaw string) *string {
	base, err := url.Parse(strings.TrimSpace(baseRaw))
	if err != nil {
		return nil
	}
	artifact, err := url.Parse(strings.TrimSpace(artifactRaw))
	if err != nil {
		return nil
	}
	if base.Scheme != artifact.Scheme || base.Host != artifact.Host {
		return nil
	}
	rel, err := filepath.Rel(filepath.Dir(base.Path), artifact.Path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil
	}
	rel = filepath.ToSlash(rel)
	return &rel
}

func jsRuntimeArtifactFromRepo(in repository.SourceRuntimeArtifact) JSRuntimeArtifact {
	return JSRuntimeArtifact{
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

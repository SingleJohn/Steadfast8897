package source

import (
	"encoding/json"

	"fyms/internal/repository"
)

const (
	JSRuntimeKindNodeDRPY = "js_node_drpy"

	JSRuntimeMethodInit     = "init"
	JSRuntimeMethodHome     = "home"
	JSRuntimeMethodCategory = "category"
	JSRuntimeMethodSearch   = "search"
	JSRuntimeMethodDetail   = "detail"
	JSRuntimeMethodPlay     = "play"
	JSRuntimeMethodProxy    = "proxy"
)

type JSRuntimeRequest struct {
	ConfigBaseURL string            `json:"configBaseUrl"`
	Engine        string            `json:"engine"`
	Rule          string            `json:"rule"`
	Method        string            `json:"method"`
	Headers       map[string]string `json:"headers,omitempty"`
	Args          map[string]any    `json:"args"`
	ProviderID    *int64            `json:"providerId"`
	ProviderKey   string            `json:"providerKey"`
	TimeoutMs     int               `json:"timeoutMs"`
}

type JSRuntimeArtifact struct {
	ID           int64   `json:"id,omitempty"`
	Kind         string  `json:"kind"`
	URL          string  `json:"url"`
	Path         string  `json:"path"`
	Bytes        int64   `json:"bytes"`
	MD5          string  `json:"md5"`
	SHA256       string  `json:"sha256"`
	TrustStatus  string  `json:"trustStatus"`
	ContentType  *string `json:"contentType,omitempty"`
	RelativePath *string `json:"relativePath,omitempty"`
}

type JSRuntimeMethodResult struct {
	OK         bool            `json:"ok"`
	Method     string          `json:"method"`
	EngineOK   bool            `json:"engineOk,omitempty"`
	EngineNote string          `json:"engineNote,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
	Error      string          `json:"error,omitempty"`
	ErrorType  string          `json:"errorType,omitempty"`
	DurationMs int64           `json:"durationMs"`
}

type JSRuntimeResponse struct {
	OK          bool                    `json:"ok"`
	Engine      string                  `json:"engine"`
	RuntimeKind string                  `json:"runtimeKind"`
	BaseURL     string                  `json:"baseUrl"`
	Artifacts   []JSRuntimeArtifact     `json:"artifacts"`
	Results     []JSRuntimeMethodResult `json:"results"`
	Logs        []string                `json:"logs"`
	DurationMs  int64                   `json:"durationMs"`
	WorkerPID   int                     `json:"workerPid,omitempty"`
}

type jsArtifactDownload struct {
	Artifact repository.SourceRuntimeArtifact
	Body     []byte
}

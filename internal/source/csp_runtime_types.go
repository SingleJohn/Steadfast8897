package source

import "encoding/json"

const (
	CSPRuntimeKindJVM = "csp_dex"

	CSPRuntimeMethodInit      = "init"
	CSPRuntimeMethodHome      = "home"
	CSPRuntimeMethodHomeVideo = "homeVideo"
	CSPRuntimeMethodCategory  = "category"
	CSPRuntimeMethodDetail    = "detail"
	CSPRuntimeMethodSearch    = "search"
	CSPRuntimeMethodPlay      = "play"
	CSPRuntimeMethodProxy     = "proxy"
)

type CSPRuntimeRequest struct {
	ConfigBaseURL string            `json:"configBaseUrl"`
	Spider        string            `json:"spider"`
	MD5           string            `json:"md5"`
	API           string            `json:"api"`
	Ext           string            `json:"ext"`
	Method        string            `json:"method"`
	Headers       map[string]string `json:"headers,omitempty"`
	Args          map[string]any    `json:"args"`
	ProviderID    *int64            `json:"providerId"`
	ProviderKey   string            `json:"providerKey"`
	TimeoutMs     int               `json:"timeoutMs"`
}

type CSPRuntimeArtifact struct {
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

type CSPDex2JarResult struct {
	OK         bool   `json:"ok"`
	Tool       string `json:"tool,omitempty"`
	InputPath  string `json:"inputPath,omitempty"`
	OutputPath string `json:"outputPath,omitempty"`
	DurationMs int64  `json:"durationMs"`
	Error      string `json:"error,omitempty"`
	ErrorType  string `json:"errorType,omitempty"`
}

type CSPSidecarResult struct {
	OK             bool            `json:"ok"`
	Method         string          `json:"method"`
	ClassName      string          `json:"className"`
	Data           json.RawMessage `json:"data,omitempty"`
	Error          string          `json:"error,omitempty"`
	ErrorType      string          `json:"errorType,omitempty"`
	DurationMs     int64           `json:"durationMs"`
	AndroidStubs   []string        `json:"androidStubs,omitempty"`
	CatVodStubs    []string        `json:"catVodStubs,omitempty"`
	NetworkBridge  string          `json:"networkBridge,omitempty"`
	UnsupportedAPI []string        `json:"unsupportedApi,omitempty"`
}

type CSPRuntimeResponse struct {
	OK          bool               `json:"ok"`
	RuntimeKind string             `json:"runtimeKind"`
	BaseURL     string             `json:"baseUrl"`
	API         string             `json:"api"`
	Method      string             `json:"method"`
	Artifact    CSPRuntimeArtifact `json:"artifact"`
	Dex2Jar     CSPDex2JarResult   `json:"dex2jar"`
	Result      CSPSidecarResult   `json:"result"`
	Data        json.RawMessage    `json:"data,omitempty"`
	Logs        []string           `json:"logs"`
	DurationMs  int64              `json:"durationMs"`
	WorkerPID   int                `json:"workerPid,omitempty"`
}

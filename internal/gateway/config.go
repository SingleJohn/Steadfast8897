package gateway

// GatewayConfig is the top-level configuration for the 302 redirect gateway.
type GatewayConfig struct {
	Sources       []EmbySourceConfig   `json:"sources"`
	PathRuleSets  []PathRuleSetConfig  `json:"path_rule_sets"`
	Backends      []BackendConfig      `json:"backends"`
	ResourcePools []ResourcePoolConfig `json:"resource_pools"`
	Observability ObservabilityConfig  `json:"observability"`
}

type EmbySourceConfig struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`

	ListenHost       string `json:"listen_host"`
	ListenPort       int    `json:"listen_port"`
	StreamPathPrefix string `json:"stream_path_prefix"`

	Upstream             EmbyUpstreamConfig `json:"upstream"`
	Routes               []RouteRuleConfig  `json:"routes"`
	DisableProxyFallback bool               `json:"disable_proxy_fallback"`
}

type EmbyUpstreamConfig struct {
	Mode     string `json:"mode"` // "external" (default) or "self"
	Host     string `json:"host"`
	BasePath string `json:"base_path"`
	ApiKey   string `json:"api_key"`
}

type RouteRuleConfig struct {
	ID       string `json:"id"`
	Enabled  bool   `json:"enabled"`
	Priority int    `json:"priority"`

	Match RouteMatchConfig `json:"match"`

	PathRuleSetID  string `json:"path_rule_set_id"`
	PoolID         string `json:"pool_id"`
	RequireMapping bool   `json:"require_mapping"`
}

type RouteMatchConfig struct {
	RealPathPrefix []string `json:"real_path_prefix"`
	RealPathRegex  []string `json:"real_path_regex"`
}

type PathRuleSetConfig struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Mappings []PathMapping `json:"mappings"`
}

type PathMapping struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type ResourcePoolConfig struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	PrimaryBackendID string `json:"primary_backend_id"`
	StandbyBackendID string `json:"standby_backend_id"`
}

type BackendConfig struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"` // s3|aliyun_cdn|gdrive|local|local_agent|pan123|115_open|115_cookie|115_sub
	Enabled bool   `json:"enabled"`

	S3         *S3BackendConfig         `json:"s3,omitempty"`
	AliyunCDN  *AliyunCDNBackendConfig  `json:"aliyun_cdn,omitempty"`
	GDrive     *GDriveBackendConfig     `json:"gdrive,omitempty"`
	Local      *LocalBackendConfig      `json:"local,omitempty"`
	LocalAgent *LocalAgentBackendConfig `json:"local_agent,omitempty"`
	Pan123     *Pan123BackendConfig     `json:"pan123,omitempty"`
	Open115    *Open115BackendConfig    `json:"115_open,omitempty"`
	Cookie115  *Cookie115BackendConfig  `json:"115_cookie,omitempty"`
	Sub115     *Sub115BackendConfig     `json:"115_sub,omitempty"`
}

type S3BackendConfig struct {
	Endpoint          string `json:"endpoint"`
	Region            string `json:"region"`
	Bucket            string `json:"bucket"`
	AccessKey         string `json:"access_key"`
	SecretKey         string `json:"secret_key"`
	SignExpiryMinutes int    `json:"sign_expiry_minutes"`
	ForcePathStyle    bool   `json:"force_path_style"`
	KeyPrefix         string `json:"key_prefix"`
}

type AliyunCDNBackendConfig struct {
	BaseURL    string        `json:"base_url"`
	PathEscape bool          `json:"path_escape"`
	Auth       CdnAuthConfig `json:"auth"`
}

type CdnAuthConfig struct {
	Enabled        bool   `json:"enabled"`
	Type           string `json:"type"`
	Secret         string `json:"secret"`
	ExpiresSeconds int    `json:"expires_seconds"`
	Rand           string `json:"rand"`
	UID            string `json:"uid"`
	ParamName      string `json:"param_name"`
}

type GDriveBackendConfig struct {
	ClientID         string `json:"client_id"`
	ClientSecret     string `json:"client_secret"`
	RefreshToken     string `json:"refresh_token"`
	BaseURL          string `json:"base_url"`
	DriveID          string `json:"drive_id"`
	IncludeAllDrives bool   `json:"include_all_drives"`
	LinkTTLSeconds   int    `json:"link_ttl_seconds"`
	CacheEnabled     bool   `json:"cache_enabled"`
	CacheTTLSeconds  int    `json:"cache_ttl_seconds"`
}

type LocalBackendConfig struct {
	BaseDir        string `json:"base_dir"`
	BaseURL        string `json:"base_url"`
	LinkTTLSeconds int    `json:"link_ttl_seconds"`
	SignSecret     string `json:"sign_secret"`
}

type LocalAgentBackendConfig struct {
	BaseDir       string `json:"base_dir"`
	PublicBaseURL string `json:"public_base_url"`
	AgentAPIURL   string `json:"agent_api_url"`
	LinkTTLSeconds int   `json:"link_ttl_seconds"`
	SignSecret    string `json:"sign_secret"`
	SyncToken     string `json:"sync_token"`
}

type Pan123BackendConfig struct {
	ClientID             string `json:"client_id"`
	ClientSecret         string `json:"client_secret"`
	RootFolderID         string `json:"root_folder_id"`
	DirectLinkMode       string `json:"direct_link_mode"`
	ComposeBaseURL       string `json:"compose_base_url"`
	ComposeHideUID       bool   `json:"compose_hide_uid"`
	LinkTTLSeconds       int    `json:"link_ttl_seconds"`
	SignEnabled          bool   `json:"sign_enabled"`
	PrivateKey           string `json:"private_key"`
	UID                  string `json:"uid"`
	ValidDurationMinutes int    `json:"valid_duration_minutes"`
	CacheEnabled         bool   `json:"cache_enabled"`
	CacheTTLSeconds      int    `json:"cache_ttl_seconds"`
}

type Open115BackendConfig struct {
	AccessToken    string `json:"access_token"`
	RefreshToken   string `json:"refresh_token"`
	RootFolderID   string `json:"root_folder_id"`
	LinkTTLSeconds int    `json:"link_ttl_seconds"`
	CacheEnabled   bool   `json:"cache_enabled"`
	CacheTTLSeconds int   `json:"cache_ttl_seconds"`
}

type Cookie115BackendConfig struct {
	RootFolderID   string `json:"root_folder_id"`
	LinkTTLSeconds int    `json:"link_ttl_seconds"`
	CacheEnabled   bool   `json:"cache_enabled"`
	CacheTTLSeconds int   `json:"cache_ttl_seconds"`
}

type Sub115BackendConfig struct {
	PrimaryBackendID string `json:"primary_backend_id"`
	SelectionStrategy string `json:"selection_strategy"`
	LinkTTLSeconds   int    `json:"link_ttl_seconds"`
	CacheEnabled     bool   `json:"cache_enabled"`
	CacheTTLSeconds  int    `json:"cache_ttl_seconds"`
}

type ObservabilityConfig struct {
	RequestLogRetentionDays int `json:"request_log_retention_days"`
	StatRetentionDays       int `json:"stat_retention_days"`
	DBBatchSize             int `json:"db_batch_size"`
	DBFlushIntervalMs       int `json:"db_flush_interval_ms"`
}

func DefaultGatewayConfig() *GatewayConfig {
	return &GatewayConfig{
		Sources:       []EmbySourceConfig{},
		PathRuleSets:  []PathRuleSetConfig{},
		Backends:      []BackendConfig{},
		ResourcePools: []ResourcePoolConfig{},
		Observability: ObservabilityConfig{
			RequestLogRetentionDays: 7,
			StatRetentionDays:       90,
			DBBatchSize:             100,
			DBFlushIntervalMs:       2000,
		},
	}
}

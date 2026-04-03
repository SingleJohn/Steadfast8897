package gateway

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// BackendAdapter builds a redirect URL for a given object key.
type BackendAdapter interface {
	Name() string
	Type() string
	BuildRedirectURL(ctx context.Context, objectKey string) (string, error)
}

// BuildBackendAdapters constructs adapters from config.
func BuildBackendAdapters(backends []BackendConfig) map[string]BackendAdapter {
	adapters := map[string]BackendAdapter{}
	for _, b := range backends {
		if !b.Enabled {
			continue
		}
		var a BackendAdapter
		switch b.Type {
		case "s3":
			if b.S3 != nil {
				a = &s3Adapter{id: b.ID, name: b.Name, cfg: *b.S3}
			}
		case "local":
			if b.Local != nil {
				a = &localAdapter{id: b.ID, name: b.Name, cfg: *b.Local}
			}
		case "local_agent":
			if b.LocalAgent != nil {
				a = &localAgentAdapter{id: b.ID, name: b.Name, cfg: *b.LocalAgent}
			}
		case "aliyun_cdn":
			if b.AliyunCDN != nil {
				a = &aliyunCDNAdapter{id: b.ID, name: b.Name, cfg: *b.AliyunCDN}
			}
		case "gdrive":
			if b.GDrive != nil {
				a = &gdriveAdapter{id: b.ID, name: b.Name, cfg: *b.GDrive}
			}
		case "pan123":
			if b.Pan123 != nil {
				a = &pan123Adapter{id: b.ID, name: b.Name, cfg: *b.Pan123}
			}
		case "115_open":
			if b.Open115 != nil {
				a = &open115Adapter{id: b.ID, name: b.Name, cfg: *b.Open115}
			}
		case "115_cookie":
			if b.Cookie115 != nil {
				a = &cookie115Adapter{id: b.ID, name: b.Name, cfg: *b.Cookie115}
			}
		}
		if a != nil {
			adapters[b.ID] = a
		}
	}
	return adapters
}

// TryPool attempts BuildRedirectURL on primary then standby backend.
func TryPool(ctx context.Context, pool ResourcePoolConfig, adapters map[string]BackendAdapter, objectKey string) (redirectURL string, backendID string, err error) {
	ids := []string{}
	if pool.PrimaryBackendID != "" {
		ids = append(ids, pool.PrimaryBackendID)
	}
	if pool.StandbyBackendID != "" {
		ids = append(ids, pool.StandbyBackendID)
	}
	var lastErr error
	for _, id := range ids {
		a, ok := adapters[id]
		if !ok {
			lastErr = fmt.Errorf("backend %s not found", id)
			continue
		}
		u, err := a.BuildRedirectURL(ctx, objectKey)
		if err != nil {
			lastErr = err
			continue
		}
		return u, id, nil
	}
	if lastErr != nil {
		return "", "", lastErr
	}
	return "", "", fmt.Errorf("no backends configured in pool %s", pool.ID)
}

// FindPool finds a resource pool by ID.
func FindPool(pools []ResourcePoolConfig, poolID string) *ResourcePoolConfig {
	for i := range pools {
		if pools[i].ID == poolID {
			return &pools[i]
		}
	}
	return nil
}

// FindPathRuleSet finds a path rule set by ID.
func FindPathRuleSet(sets []PathRuleSetConfig, id string) *PathRuleSetConfig {
	for i := range sets {
		if sets[i].ID == id {
			return &sets[i]
		}
	}
	return nil
}

// SortRoutesByPriority returns routes sorted by priority (ascending).
func SortRoutesByPriority(routes []RouteRuleConfig) []RouteRuleConfig {
	sorted := make([]RouteRuleConfig, len(routes))
	copy(sorted, routes)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})
	return sorted
}

// --- S3 Adapter (AWS Signature V4 presigning) ---

type s3Adapter struct {
	id   string
	name string
	cfg  S3BackendConfig
}

func (a *s3Adapter) Name() string { return a.name }
func (a *s3Adapter) Type() string { return "s3" }

func (a *s3Adapter) BuildRedirectURL(_ context.Context, objectKey string) (string, error) {
	expiry := a.cfg.SignExpiryMinutes * 60
	if expiry <= 0 {
		expiry = 3600
	}

	key := strings.TrimPrefix(a.cfg.KeyPrefix+objectKey, "/")
	region := a.cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	endpoint := strings.TrimRight(a.cfg.Endpoint, "/")
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse S3 endpoint: %w", err)
	}

	var host string
	var pathPrefix string
	if a.cfg.ForcePathStyle {
		host = parsed.Host
		pathPrefix = "/" + a.cfg.Bucket
	} else {
		host = a.cfg.Bucket + "." + parsed.Host
		pathPrefix = ""
	}

	canonPath := pathPrefix + "/" + key

	now := time.Now().UTC()
	datestamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")
	credScope := fmt.Sprintf("%s/%s/s3/aws4_request", datestamp, region)
	credential := fmt.Sprintf("%s/%s", a.cfg.AccessKey, credScope)

	q := url.Values{}
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", credential)
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", fmt.Sprintf("%d", expiry))
	q.Set("X-Amz-SignedHeaders", "host")

	canonQueryString := q.Encode()
	canonHeaders := fmt.Sprintf("host:%s\n", host)
	signedHeaders := "host"

	canonRequest := fmt.Sprintf("GET\n%s\n%s\n%s\n%s\nUNSIGNED-PAYLOAD",
		canonPath, canonQueryString, canonHeaders, signedHeaders)

	h := sha256.Sum256([]byte(canonRequest))
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s", amzDate, credScope, hex.EncodeToString(h[:]))

	sigKey := s3DeriveSigningKey(a.cfg.SecretKey, datestamp, region, "s3")
	mac := hmac.New(sha256.New, sigKey)
	mac.Write([]byte(stringToSign))
	signature := hex.EncodeToString(mac.Sum(nil))

	q.Set("X-Amz-Signature", signature)

	finalURL := fmt.Sprintf("%s://%s%s?%s", parsed.Scheme, host, canonPath, q.Encode())
	return finalURL, nil
}

func s3DeriveSigningKey(secret, date, region, service string) []byte {
	kDate := s3HmacSHA256([]byte("AWS4"+secret), []byte(date))
	kRegion := s3HmacSHA256(kDate, []byte(region))
	kService := s3HmacSHA256(kRegion, []byte(service))
	return s3HmacSHA256(kService, []byte("aws4_request"))
}

func s3HmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// --- Local Adapter ---

type localAdapter struct {
	id   string
	name string
	cfg  LocalBackendConfig
}

func (a *localAdapter) Name() string { return a.name }
func (a *localAdapter) Type() string { return "local" }

func (a *localAdapter) BuildRedirectURL(_ context.Context, objectKey string) (string, error) {
	if a.cfg.BaseURL == "" {
		return "", fmt.Errorf("local backend %s: base_url is empty", a.id)
	}
	baseURL := strings.TrimRight(a.cfg.BaseURL, "/")
	path := "/" + strings.TrimLeft(objectKey, "/")

	if a.cfg.SignSecret != "" {
		ttl := a.cfg.LinkTTLSeconds
		if ttl <= 0 {
			ttl = 3600
		}
		expires := time.Now().Unix() + int64(ttl)
		mac := hmac.New(sha256.New, []byte(a.cfg.SignSecret))
		mac.Write([]byte(fmt.Sprintf("%s%d", path, expires)))
		sig := hex.EncodeToString(mac.Sum(nil))
		return fmt.Sprintf("%s%s?expires=%d&sig=%s", baseURL, path, expires, sig), nil
	}
	return baseURL + path, nil
}

// --- Local Agent Adapter ---

type localAgentAdapter struct {
	id   string
	name string
	cfg  LocalAgentBackendConfig
}

func (a *localAgentAdapter) Name() string { return a.name }
func (a *localAgentAdapter) Type() string { return "local_agent" }

func (a *localAgentAdapter) BuildRedirectURL(_ context.Context, objectKey string) (string, error) {
	if a.cfg.PublicBaseURL == "" {
		return "", fmt.Errorf("local_agent backend %s: public_base_url is empty", a.id)
	}
	baseURL := strings.TrimRight(a.cfg.PublicBaseURL, "/")
	path := "/" + strings.TrimLeft(objectKey, "/")

	secret := a.cfg.SignSecret
	if secret == "" {
		return baseURL + path, nil
	}

	ttl := a.cfg.LinkTTLSeconds
	if ttl <= 0 {
		ttl = 3600
	}
	expires := time.Now().Unix() + int64(ttl)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%s%d", path, expires)))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s%s?expires=%d&sig=%s", baseURL, path, expires, sig), nil
}

// --- Aliyun CDN Adapter ---

type aliyunCDNAdapter struct {
	id   string
	name string
	cfg  AliyunCDNBackendConfig
}

func (a *aliyunCDNAdapter) Name() string { return a.name }
func (a *aliyunCDNAdapter) Type() string { return "aliyun_cdn" }

func (a *aliyunCDNAdapter) BuildRedirectURL(_ context.Context, objectKey string) (string, error) {
	baseURL := strings.TrimRight(a.cfg.BaseURL, "/")
	path := "/" + strings.TrimLeft(objectKey, "/")

	if a.cfg.PathEscape {
		segments := strings.Split(path, "/")
		for i, s := range segments {
			segments[i] = url.PathEscape(s)
		}
		path = strings.Join(segments, "/")
	}

	if !a.cfg.Auth.Enabled {
		return baseURL + path, nil
	}

	expiry := a.cfg.Auth.ExpiresSeconds
	if expiry <= 0 {
		expiry = 1800
	}
	ts := time.Now().Unix() + int64(expiry)
	tsHex := fmt.Sprintf("%x", ts)

	key := a.cfg.Auth.Secret
	randStr := a.cfg.Auth.Rand
	if randStr == "" {
		randStr = "0"
	}
	uid := a.cfg.Auth.UID
	if uid == "" {
		uid = "0"
	}
	paramName := a.cfg.Auth.ParamName
	if paramName == "" {
		paramName = "auth_key"
	}

	switch a.cfg.Auth.Type {
	case "a", "":
		toSign := fmt.Sprintf("%s-%s-%s-%s-%s", path, tsHex, randStr, uid, key)
		h := md5.Sum([]byte(toSign))
		authStr := fmt.Sprintf("%s-%s-%s-%s", tsHex, randStr, uid, hex.EncodeToString(h[:]))
		return fmt.Sprintf("%s%s?%s=%s", baseURL, path, paramName, authStr), nil
	default:
		return baseURL + path, nil
	}
}

// --- Stub adapters for backends that need external API integration ---

type gdriveAdapter struct {
	id   string
	name string
	cfg  GDriveBackendConfig
}

func (a *gdriveAdapter) Name() string { return a.name }
func (a *gdriveAdapter) Type() string { return "gdrive" }
func (a *gdriveAdapter) BuildRedirectURL(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("gdrive backend %s: requires OAuth token refresh implementation", a.id)
}

type pan123Adapter struct {
	id   string
	name string
	cfg  Pan123BackendConfig
}

func (a *pan123Adapter) Name() string { return a.name }
func (a *pan123Adapter) Type() string { return "pan123" }

func (a *pan123Adapter) BuildRedirectURL(_ context.Context, objectKey string) (string, error) {
	if a.cfg.DirectLinkMode == "compose" {
		linkURL, err := a.buildComposedURL(objectKey)
		if err != nil {
			return "", err
		}
		if a.cfg.SignEnabled {
			return a.signURL(linkURL)
		}
		return linkURL, nil
	}
	return "", fmt.Errorf("pan123 backend %s: api mode requires full API integration (use compose mode)", a.id)
}

func (a *pan123Adapter) buildComposedURL(objectKey string) (string, error) {
	escaped := pan123EscapeObjectKey(objectKey)
	if escaped == "" {
		return "", fmt.Errorf("object_key is empty")
	}
	uid := strings.TrimSpace(a.cfg.UID)
	composeBase := strings.TrimSpace(a.cfg.ComposeBaseURL)

	if composeBase == "" {
		if uid == "" {
			return "", fmt.Errorf("uid is empty for compose mode")
		}
		return fmt.Sprintf("https://%s.v.123pan.cn/%s/%s", uid, uid, escaped), nil
	}

	if a.cfg.ComposeHideUID {
		return pan123JoinURL(composeBase, escaped), nil
	}
	if uid == "" {
		return "", fmt.Errorf("uid is empty for compose mode")
	}
	return pan123JoinURL(composeBase, url.PathEscape(uid), escaped), nil
}

func (a *pan123Adapter) signURL(originURL string) (string, error) {
	privateKey := strings.TrimSpace(a.cfg.PrivateKey)
	uid := strings.TrimSpace(a.cfg.UID)
	if privateKey == "" || uid == "" {
		return "", fmt.Errorf("sign is enabled but private_key or uid is empty")
	}

	u, err := url.Parse(originURL)
	if err != nil {
		return "", err
	}

	validMinutes := a.cfg.ValidDurationMinutes
	if validMinutes <= 0 {
		validMinutes = 30
	}
	expireTime := time.Now().Unix() + int64(validMinutes)*60
	randInt := time.Now().UnixNano() & 0x7fffffffffffffff

	raw := fmt.Sprintf("%s-%d-%d-%s-%s", u.Path, expireTime, randInt, uid, privateKey)
	sign := fmt.Sprintf("%x", md5.Sum([]byte(raw)))
	authKey := fmt.Sprintf("%d-%d-%s-%s", expireTime, randInt, uid, sign)

	query := u.Query()
	query.Set("auth_key", authKey)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func pan123EscapeObjectKey(key string) string {
	normalized := strings.Trim(key, "/")
	if normalized == "" {
		return ""
	}
	parts := strings.Split(normalized, "/")
	escaped := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		escaped = append(escaped, url.PathEscape(part))
	}
	return strings.Join(escaped, "/")
}

func pan123JoinURL(base string, segs ...string) string {
	out := base
	for _, seg := range segs {
		seg = strings.Trim(seg, "/")
		if seg == "" {
			continue
		}
		if strings.HasSuffix(out, "/") {
			out += seg
		} else {
			out += "/" + seg
		}
	}
	return out
}

type open115Adapter struct {
	id   string
	name string
	cfg  Open115BackendConfig
}

func (a *open115Adapter) Name() string { return a.name }
func (a *open115Adapter) Type() string { return "115_open" }
func (a *open115Adapter) BuildRedirectURL(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("115_open backend %s: requires API integration", a.id)
}

type cookie115Adapter struct {
	id   string
	name string
	cfg  Cookie115BackendConfig
}

func (a *cookie115Adapter) Name() string { return a.name }
func (a *cookie115Adapter) Type() string { return "115_cookie" }
func (a *cookie115Adapter) BuildRedirectURL(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("115_cookie backend %s: requires cookie management", a.id)
}

package system

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"fyms/internal/config"
	"fyms/internal/services"
)

func getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	if udp, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return udp.IP.String()
	}
	return "127.0.0.1"
}

func embyOperatingSystem() string {
	switch runtime.GOOS {
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	case "darwin":
		return "OSX"
	default:
		return runtime.GOOS
	}
}

func requestScheme(c *gin.Context) string {
	forwarded := strings.ToLower(strings.TrimSpace(strings.SplitN(c.GetHeader("X-Forwarded-Proto"), ",", 2)[0]))
	if forwarded == "http" || forwarded == "https" {
		return forwarded
	}
	if c.Request.TLS != nil {
		return "https"
	}
	return "http"
}

func requestHost(c *gin.Context) string {
	if forwarded := strings.TrimSpace(strings.SplitN(c.GetHeader("X-Forwarded-Host"), ",", 2)[0]); forwarded != "" {
		return forwarded
	}
	return strings.TrimSpace(c.Request.Host)
}

func applyRequestAddresses(c *gin.Context, state *AppState, info gin.H) {
	scheme := requestScheme(c)
	host := requestHost(c)
	if host == "" {
		host = net.JoinHostPort(getLocalIP(), strconv.Itoa(state.Config.Port))
	}
	address := scheme + "://" + host

	info["WanAddress"] = address
	info["RemoteAddresses"] = []string{address}
	info["SupportsHttps"] = scheme == "https"
	if scheme != "https" {
		info["HttpsPortNumber"] = 0
		return
	}

	httpsPort := 443
	if _, portText, err := net.SplitHostPort(host); err == nil {
		if port, err := strconv.Atoi(portText); err == nil {
			httpsPort = port
		}
	}
	info["HttpsPortNumber"] = httpsPort
}

func systemInfo(ctx context.Context, state *AppState, public bool) gin.H {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	updateStatus := state.Updater.GetStatus(context.Background())
	branding := services.LoadBrandingConfig(ctx, state.DB, state.Config)

	port := state.Config.Port
	localAddress := "http://" + net.JoinHostPort(getLocalIP(), strconv.Itoa(port))
	info := gin.H{
		"SystemUpdateLevel":                    "Release",
		"OperatingSystem":                      embyOperatingSystem(),
		"HasImageEnhancers":                    false,
		"SupportsLibraryMonitor":               true,
		"SupportsLocalPortConfiguration":       false,
		"SupportsWakeServer":                   false,
		"WebSocketPortNumber":                  port,
		"CompletedInstallations":               []interface{}{},
		"CanSelfRestart":                       true,
		"CanSelfUpdate":                        true,
		"ProgramDataPath":                      state.Config.DataDir,
		"HttpServerPortNumber":                 port,
		"SupportsHttps":                        false,
		"HttpsPortNumber":                      0,
		"SupportsAutoRunAtStartup":             false,
		"HardwareAccelerationRequiresPremiere": false,
		"WakeOnLanInfo":                        []interface{}{},
		"IsInMaintenanceMode":                  false,
		"LocalAddress":                         localAddress,
		"LocalAddresses":                       []string{localAddress},
		"RemoteAddresses":                      []interface{}{},
		"ServerName":                           branding.ServerName,
		"Version":                              state.Config.Version,
		"Id":                                   state.Config.ServerID,
		"ProductName":                          "FYMS",
		"StartupWizardCompleted":               true,
	}
	if branding.IconURL != "" {
		info["BrandIconUrl"] = branding.IconURL
	}

	if !public {
		info["OperatingSystemDisplayName"] = fmt.Sprintf("%s %s", runtime.GOOS, runtime.GOARCH)
		info["HasPendingRestart"] = updateStatus.Status == "pulling" || updateStatus.Status == "recreating" || updateStatus.Status == "restarting"
		info["IsShuttingDown"] = false
		info["CanLaunchWebBrowser"] = false
		info["HasUpdateAvailable"] = updateStatus.HasUpdate
		info["UpdateStatus"] = updateStatus
		info["TranscodingTempPath"] = ""
		info["LogPath"] = ""
		info["InternalMetadataPath"] = ""
		info["CachePath"] = state.Config.CacheDir
		info["ProcessId"] = os.Getpid()
		info["HeapAllocatedBytes"] = m.Alloc
		info["SystemTotalBytes"] = m.Sys
		info["ServerDateTime"] = time.Now().UTC().Format(time.RFC3339)
		if config.BuildCommit != "" {
			info["BuildCommit"] = config.BuildCommit
		}
		if config.BuildTime != "" {
			info["BuildTime"] = config.BuildTime
		}
		if config.BuildRepo != "" {
			info["BuildRepo"] = config.BuildRepo
		}
	}

	return info
}

func getSystemInfo(c *gin.Context) {
	state := GetState(c)
	info := systemInfo(c.Request.Context(), state, false)
	applyRequestAddresses(c, state, info)
	applyEmbyOfficialOverrides(c, info)
	c.JSON(http.StatusOK, info)
}

func getSystemInfoPublic(c *gin.Context) {
	state := GetState(c)
	info := systemInfo(c.Request.Context(), state, true)
	applyRequestAddresses(c, state, info)
	applyEmbyOfficialOverrides(c, info)
	c.JSON(http.StatusOK, info)
}

// isEmbyOfficialClient 识别 Emby 官方客户端，用于伪装 Version/ProductName 通过其严格校验。
// 命中条件：UA 含 Emby/、Emby Theater、Emby for、EmbyAndroid；
// 或 Authorization 头里 Client 是 Emby Theater / Emby for ... / Emby Web / Emby Mobile。
// 前端用 Client="Media Server Web"，不会命中。
func isEmbyOfficialClient(c *gin.Context) bool {
	// Emby JS SDK 通用行为：所有 Emby 官方客户端 (Mac/iOS/Android/Web) 都会
	// 设 X-Emby-Client 头。FYMS 前端不设此头，第三方客户端 (Infuse/Yamby
	// /Hills 等) 也不用 Emby JS SDK，所以不会有这头。最可靠的命中条件。
	if c.GetHeader("X-Emby-Client") != "" {
		return true
	}
	ua := c.GetHeader("User-Agent")
	if strings.Contains(ua, "Emby/") ||
		strings.Contains(ua, "Emby Theater") ||
		strings.Contains(ua, "Emby for ") ||
		strings.Contains(ua, "EmbyAndroid") {
		return true
	}
	auth := c.GetHeader("X-Emby-Authorization")
	if auth == "" {
		auth = c.GetHeader("Authorization")
	}
	return strings.Contains(auth, `Client="Emby Theater"`) ||
		strings.Contains(auth, `Client="Emby for `) ||
		strings.Contains(auth, `Client="Emby Web"`) ||
		strings.Contains(auth, `Client="Emby Mobile"`)
}

func applyEmbyOfficialOverrides(c *gin.Context, info gin.H) {
	if !isEmbyOfficialClient(c) {
		return
	}
	// 必须严格等于 4.7.14：Emby Mobile (com.emby.mobile) connectionmanager.js 里
	// compareVersions 把返回值当 boolean 用，-1/1 都是 truthy → 任何 !== "4.7.14"
	// 都会被判定为"需要更新"。这是该客户端的 bug，只能精确匹配 minServerVersion。
	info["Version"] = "4.7.14"
	info["ProductName"] = "Emby Server"
}

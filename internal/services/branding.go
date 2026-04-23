package services

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/config"
)

const (
	BrandServerNameKey = "brand_server_name"
	BrandIconSVGKey    = "brand_icon_svg"
)

type BrandingConfig struct {
	ServerName string `json:"ServerName"`
	IconURL    string `json:"IconUrl,omitempty"`
	HasIcon    bool   `json:"HasIcon"`
}

func LoadBrandingConfig(ctx context.Context, pool *pgxpool.Pool, cfg *config.AppConfig) BrandingConfig {
	name := strings.TrimSpace(readSystemConfigValue(ctx, pool, BrandServerNameKey))
	if name == "" {
		name = cfg.ServerName
	}

	iconSVG := strings.TrimSpace(readSystemConfigValue(ctx, pool, BrandIconSVGKey))
	iconURL := ""
	if IsSVGDocument(iconSVG) {
		iconURL = SVGDataURL(iconSVG)
	}

	return BrandingConfig{
		ServerName: name,
		IconURL:    iconURL,
		HasIcon:    iconURL != "",
	}
}

func IsSVGDocument(raw string) bool {
	s := strings.TrimSpace(raw)
	return s != "" && strings.Contains(strings.ToLower(s), "<svg")
}

func SVGDataURL(svg string) string {
	if !IsSVGDocument(svg) {
		return ""
	}
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}

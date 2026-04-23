package services

import "encoding/json"

type RefreshScope string

const (
	RefreshScopeMetadata RefreshScope = "metadata"
	RefreshScopeImages   RefreshScope = "images"
	RefreshScopeSubtree  RefreshScope = "subtree"
)

type RefreshSource string

const (
	RefreshSourceScan    RefreshSource = "scan"
	RefreshSourceFS      RefreshSource = "fsnotify"
	RefreshSourceSidecar RefreshSource = "sidecar"
	RefreshSourceManual  RefreshSource = "manual"
)

type RefreshOptions struct {
	ReplaceAllMetadata bool `json:"replace_all_metadata"`
	ReplaceAllImages   bool `json:"replace_all_images"`
	ValidateOnly       bool `json:"validate_only"`
	AllowRemote        bool `json:"allow_remote"`
	RefreshSubtree     bool `json:"refresh_subtree"`
}

func DefaultRefreshOptionsForSource(source RefreshSource) RefreshOptions {
	switch source {
	case RefreshSourceScan:
		return RefreshOptions{AllowRemote: false}
	case RefreshSourceSidecar:
		return RefreshOptions{AllowRemote: false}
	case RefreshSourceFS:
		return RefreshOptions{AllowRemote: false}
	default:
		return RefreshOptions{AllowRemote: true}
	}
}

func (o RefreshOptions) Marshal() string {
	data, err := json.Marshal(o)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func ParseRefreshOptions(raw string) RefreshOptions {
	if raw == "" {
		return RefreshOptions{}
	}
	var opts RefreshOptions
	if err := json.Unmarshal([]byte(raw), &opts); err != nil {
		return RefreshOptions{}
	}
	return opts
}

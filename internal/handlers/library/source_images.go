package library

import (
	"crypto/sha1"
	"fmt"
	"strings"

	"fyms/internal/repository"
)

func sourceImageTag(providerID int64, publicUUID, imageURL string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(imageURL)))
	return fmt.Sprintf("source-%d-%s-%x", providerID, publicUUID, sum[:8])
}

func sourceImageURLForType(item repository.SourceItem, imageType string) string {
	switch strings.ToLower(strings.TrimSpace(imageType)) {
	case "backdrop", "banner":
		if item.BackdropURL != nil && strings.TrimSpace(*item.BackdropURL) != "" {
			return strings.TrimSpace(*item.BackdropURL)
		}
	}
	if item.PosterURL != nil && strings.TrimSpace(*item.PosterURL) != "" {
		return strings.TrimSpace(*item.PosterURL)
	}
	return ""
}

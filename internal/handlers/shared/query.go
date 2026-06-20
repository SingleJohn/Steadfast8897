package shared

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

// QueryAny returns the first non-empty query parameter value among the given keys.
func QueryAny(c *gin.Context, keys ...string) string {
	for _, k := range keys {
		if v := c.Query(k); v != "" {
			return v
		}
	}
	return ""
}

func StrOrPtr(a *string, def string) string {
	if a == nil {
		return def
	}
	return *a
}

func ParseCompatBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "y":
		return true, nil
	case "false", "0", "no", "n":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value %q", value)
	}
}

func EmbyTotalRecordCount(c *gin.Context, actual int64) int64 {
	v := strings.TrimSpace(QueryAny(c, "EnableTotalRecordCount", "enableTotalRecordCount", "enabletotalrecordcount"))
	if strings.EqualFold(v, "false") {
		return 0
	}
	return actual
}

// NormalizeItemType maps client-provided item type casing to FYMS canonical values.
func NormalizeItemType(s string) string {
	if v, ok := itemTypeCanonical[strings.ToLower(strings.TrimSpace(s))]; ok {
		return v
	}
	return s
}

var itemTypeCanonical = map[string]string{
	"movie":            "Movie",
	"series":           "Series",
	"episode":          "Episode",
	"season":           "Season",
	"boxset":           "BoxSet",
	"playlist":         "Playlist",
	"musicvideo":       "MusicVideo",
	"video":            "Video",
	"audio":            "Audio",
	"folder":           "Folder",
	"collectionfolder": "CollectionFolder",
	"userview":         "UserView",
	"musicalbum":       "MusicAlbum",
	"musicartist":      "MusicArtist",
}

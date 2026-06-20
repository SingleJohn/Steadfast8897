package handlers

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"fyms/internal/dto"
)

func baseItemToEmbyMap(item dto.BaseItemDto) gin.H {
	raw, err := json.Marshal(item)
	if err != nil {
		return gin.H{"Id": item.ID, "Name": item.Name, "Type": item.Type}
	}
	var out gin.H
	if err := json.Unmarshal(raw, &out); err != nil {
		return gin.H{"Id": item.ID, "Name": item.Name, "Type": item.Type}
	}
	applyBaseItemEmbyDefaults(out)
	return out
}

func baseItemsToEmbyMaps(items []dto.BaseItemDto) []gin.H {
	out := make([]gin.H, 0, len(items))
	for i := range items {
		out = append(out, baseItemToEmbyMap(items[i]))
	}
	return out
}

func mediaSourcesToEmbyMaps(sources []dto.MediaSourceInfo) []gin.H {
	out := make([]gin.H, 0, len(sources))
	for i := range sources {
		raw, err := json.Marshal(sources[i])
		if err != nil {
			continue
		}
		var m gin.H
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		applyMediaSourceEmbyDefaults(m)
		out = append(out, m)
	}
	return out
}

func applyBaseItemEmbyDefaults(item gin.H) {
	if item == nil {
		return
	}
	id := stringValue(item["Id"])
	name := stringValue(item["Name"])
	itemType := stringValue(item["Type"])
	isPlayable := itemType == "Movie" || itemType == "Episode" || itemType == "Video"
	isFolder := itemType == "CollectionFolder" || itemType == "Folder" || itemType == "Series" || itemType == "Season" || itemType == "BoxSet" || itemType == "Playlist"

	ensureString(item, "ServerId", "")
	ensureString(item, "Name", name)
	ensureString(item, "SortName", strings.ToLower(name))
	ensureString(item, "ForcedSortName", name)
	ensureString(item, "Etag", id)
	ensureString(item, "PresentationUniqueKey", id)
	ensureString(item, "DisplayPreferencesId", id)
	ensureBool(item, "CanDelete", false)
	ensureBool(item, "CanDownload", isPlayable)
	ensureBool(item, "SupportsSync", true)
	ensureBool(item, "LockData", false)
	ensureString(item, "LocationType", "FileSystem")
	if isPlayable {
		ensureString(item, "MediaType", "Video")
		ensureString(item, "PlayAccess", "Full")
	}
	if _, ok := item["IsFolder"]; !ok && isFolder {
		item["IsFolder"] = true
	}
	ensureMap(item, "ImageTags")
	ensureStringSlice(item, "BackdropImageTags")
	ensureMap(item, "ProviderIds")
	ensureSlice(item, "ExternalUrls")
	ensureStringSlice(item, "Taglines")
	ensureSlice(item, "RemoteTrailers")
	ensureStringSlice(item, "LockedFields")
	ensureStringSlice(item, "ProductionLocations")
	ensureSlice(item, "TagItems")
	ensureUserData(item)
	if _, ok := item["DateCreated"]; !ok {
		item["DateCreated"] = embyZeroTime()
	}
	if _, ok := item["DateModified"]; !ok {
		item["DateModified"] = item["DateCreated"]
	}
	if isPlayable {
		applyMediaSourcesValueDefaults(item)
	}
}

func applyMediaSourcesValueDefaults(item gin.H) {
	raw, ok := item["MediaSources"].([]interface{})
	if !ok {
		return
	}
	out := make([]gin.H, 0, len(raw))
	for _, entry := range raw {
		m, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		h := gin.H(m)
		applyMediaSourceEmbyDefaults(h)
		out = append(out, h)
	}
	item["MediaSources"] = out
}

func applyMediaSourceEmbyDefaults(src gin.H) {
	if src == nil {
		return
	}
	ensureString(src, "Protocol", "File")
	ensureString(src, "Type", "Default")
	ensureString(src, "Container", "")
	ensureString(src, "Name", "Default")
	ensureString(src, "ETag", stringValue(src["Id"]))
	ensureBool(src, "IsRemote", false)
	ensureBool(src, "HasMixedProtocols", false)
	ensureBool(src, "SupportsDirectPlay", true)
	ensureBool(src, "SupportsDirectStream", true)
	ensureBool(src, "SupportsTranscoding", false)
	ensureBool(src, "SupportsProbing", true)
	ensureBool(src, "IsInfiniteStream", false)
	ensureBool(src, "RequiresOpening", false)
	ensureBool(src, "RequiresClosing", false)
	ensureBool(src, "RequiresLooping", false)
	ensureBool(src, "ReadAtNativeFramerate", false)
	ensureBool(src, "AddApiKeyToDirectStreamUrl", false)
	ensureString(src, "VideoType", "VideoFile")
	ensureMap(src, "RequiredHttpHeaders")
	ensureStringSlice(src, "Formats")
	ensureSlice(src, "MediaStreams")
	ensureSlice(src, "MediaAttachments")
	ensureSlice(src, "Chapters")
}

func applyCollectionFolderDefaults(entry gin.H, collectionType string, keepCollectionType bool) gin.H {
	if entry == nil {
		entry = gin.H{}
	}
	if _, ok := entry["Type"]; !ok {
		entry["Type"] = "CollectionFolder"
	}
	entry["IsFolder"] = true
	if keepCollectionType && collectionType != "" {
		entry["CollectionType"] = collectionType
	} else if !keepCollectionType {
		delete(entry, "CollectionType")
	}
	applyBaseItemEmbyDefaults(entry)
	entry["CanDownload"] = false
	return entry
}

func ensureUserData(item gin.H) {
	ud, ok := item["UserData"].(map[string]interface{})
	if !ok {
		if h, hok := item["UserData"].(gin.H); hok {
			ud = h
		} else {
			ud = map[string]interface{}{}
			item["UserData"] = ud
		}
	}
	if _, ok := ud["PlaybackPositionTicks"]; !ok {
		ud["PlaybackPositionTicks"] = float64(0)
	}
	if _, ok := ud["PlayCount"]; !ok {
		ud["PlayCount"] = float64(0)
	}
	if _, ok := ud["IsFavorite"]; !ok {
		ud["IsFavorite"] = false
	}
	if _, ok := ud["Played"]; !ok {
		ud["Played"] = false
	}
}

func ensureString(m gin.H, key, value string) {
	if _, ok := m[key]; !ok {
		m[key] = value
	}
}

func ensureBool(m gin.H, key string, value bool) {
	if _, ok := m[key]; !ok {
		m[key] = value
	}
}

func ensureMap(m gin.H, key string) {
	if _, ok := m[key]; !ok || m[key] == nil {
		m[key] = gin.H{}
	}
}

func ensureSlice(m gin.H, key string) {
	if _, ok := m[key]; !ok || m[key] == nil {
		m[key] = []interface{}{}
	}
}

func ensureStringSlice(m gin.H, key string) {
	if _, ok := m[key]; !ok || m[key] == nil {
		m[key] = []string{}
	}
}

func stringValue(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func embyZeroTime() string {
	return time.Unix(0, 0).UTC().Format("2006-01-02T15:04:05.0000000") + "Z"
}

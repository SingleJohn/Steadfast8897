package source

import "github.com/google/uuid"

var (
	sourceItemNamespace = uuid.MustParse("6f1a7f4b-2b8b-4e4a-9f4e-9a64d2b6d7b1")
	playSourceNamespace = uuid.MustParse("9b9a0c6d-04f3-4b51-9d9e-6f64f9e8e4a2")
	sourceLibNamespace  = uuid.MustParse("0b90df4d-9cb7-4bde-91a5-2d4f0d0fdb9c")
	episodeNamespace    = uuid.MustParse("3d8f2ef8-5f1e-45f4-8ef8-4c38d7d9e2aa")
)

func SourceItemPublicUUID(siteKey, sourceItemID string) string {
	return uuid.NewSHA1(sourceItemNamespace, []byte(siteKey+"\x00"+sourceItemID)).String()
}

func PlaySourcePublicUUID(sourceItemUUID, lineName, episodeKey string) string {
	return uuid.NewSHA1(playSourceNamespace, []byte(sourceItemUUID+"\x00"+lineName+"\x00"+episodeKey)).String()
}

func SourceLibraryViewPublicUUID(dimension, matchValue string) string {
	return uuid.NewSHA1(sourceLibNamespace, []byte(dimension+"\x00"+matchValue)).String()
}

func EpisodePublicUUID(sourceItemUUID, episodeKey string) string {
	return uuid.NewSHA1(episodeNamespace, []byte(sourceItemUUID+"\x00"+episodeKey)).String()
}

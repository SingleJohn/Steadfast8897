package services

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	libraryTypeMovies  = "movies"
	libraryTypeTVShows = "tvshows"
	libraryTypeMixed   = "mixed"
)

type showEntry struct {
	name       string
	fullPath   string
	videoPaths []string
}

type mixedScanEntries struct {
	movies []movieEntry
	shows  []showEntry
}

func collectShowEntries(dir string, results *[]showEntry) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") {
			continue
		}
		if !entry.IsDir() {
			continue
		}
		fullPath := filepath.Join(dir, name)
		if isShowDir(fullPath) {
			*results = append(*results, showEntry{
				name:       name,
				fullPath:   fullPath,
				videoPaths: collectShowVideoPaths(fullPath),
			})
		} else {
			collectShowEntries(fullPath, results)
		}
	}
}

func collectShowDirs(dir string, results *[][2]string) {
	var shows []showEntry
	collectShowEntries(dir, &shows)
	for _, show := range shows {
		*results = append(*results, [2]string{show.name, show.fullPath})
	}
}

func collectShowVideoPaths(showPath string) []string {
	entries, err := os.ReadDir(showPath)
	if err != nil {
		return nil
	}
	var paths []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") {
			continue
		}
		fullPath := filepath.Join(showPath, name)
		if entry.IsDir() {
			if extractSeasonNumber(name) < 0 {
				continue
			}
			seasonEntries, err := os.ReadDir(fullPath)
			if err != nil {
				continue
			}
			for _, se := range seasonEntries {
				if se.IsDir() || strings.HasPrefix(se.Name(), ".") || strings.HasPrefix(se.Name(), "@") {
					continue
				}
				if IsVideoExt(filepath.Ext(se.Name())) {
					paths = append(paths, filepath.Join(fullPath, se.Name()))
				}
			}
			continue
		}
		if IsVideoExt(filepath.Ext(name)) {
			paths = append(paths, fullPath)
		}
	}
	return paths
}

func collectMixedEntries(dir string, results *mixedScanEntries) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") {
			continue
		}
		fullPath := filepath.Join(dir, name)
		if entry.IsDir() {
			if IsExtrasDirName(name) || looksLikeSeasonDir(name) {
				continue
			}
			if mixedLooksLikeShowDir(fullPath) {
				results.shows = append(results.shows, showEntry{
					name:       name,
					fullPath:   fullPath,
					videoPaths: collectShowVideoPaths(fullPath),
				})
				continue
			}
			if movie, ok := mixedMovieEntryForDir(name, fullPath); ok {
				results.movies = append(results.movies, movie)
				continue
			}
			collectMixedEntries(fullPath, results)
			continue
		}
		if IsVideoExt(filepath.Ext(name)) && !IsInExtrasFolder(fullPath) {
			results.movies = append(results.movies, movieEntry{name: name, fullPath: fullPath, isDir: false})
		}
	}
}

func mixedLooksLikeShowDir(path string) bool {
	if looksLikeSeasonDir(filepath.Base(path)) || isBdmvMovieDir(path) {
		return false
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	directVideos := 0
	episodeLike := 0
	for _, entry := range entries {
		name := entry.Name()
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, ".") || strings.HasPrefix(lower, "@") {
			continue
		}
		if entry.IsDir() {
			if looksLikeSeasonDir(name) {
				return true
			}
			continue
		}
		if lower == "tvshow.nfo" {
			return true
		}
		if lower == "movie.nfo" {
			return false
		}
		if !IsVideoExt(filepath.Ext(name)) {
			continue
		}
		directVideos++
		if ParseEpisodeInfo(name) != nil {
			episodeLike++
		}
	}
	return episodeLike > 0 && (episodeLike >= 2 || directVideos == episodeLike)
}

func mixedMovieEntryForDir(name, fullPath string) (movieEntry, bool) {
	if isBdmvMovieDir(fullPath) {
		vids := collectBdmvVideos(fullPath)
		paths := make([]string, 0, len(vids))
		for _, v := range vids {
			paths = append(paths, v[1])
		}
		return movieEntry{name: name, fullPath: fullPath, isDir: true, videoPaths: paths}, true
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return movieEntry{}, false
	}
	hasMovieNfo := false
	var videoPaths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		entryName := entry.Name()
		lower := strings.ToLower(entryName)
		if lower == "movie.nfo" {
			hasMovieNfo = true
			continue
		}
		if IsVideoExt(filepath.Ext(entryName)) {
			videoPaths = append(videoPaths, filepath.Join(fullPath, entryName))
		}
	}
	if len(videoPaths) == 0 {
		return movieEntry{}, false
	}
	if hasMovieNfo {
		return movieEntry{name: name, fullPath: fullPath, isDir: true, videoPaths: videoPaths}, true
	}
	for _, p := range videoPaths {
		if ParseEpisodeInfo(filepath.Base(p)) != nil {
			return movieEntry{}, false
		}
	}
	return movieEntry{name: name, fullPath: fullPath, isDir: true, videoPaths: videoPaths}, true
}

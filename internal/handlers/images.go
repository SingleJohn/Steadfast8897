package handlers

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/gin-gonic/gin"

	"fyms/internal/config"
	"fyms/internal/models"
)

var imageSemaphore = make(chan struct{}, 3)

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// RegisterImageRoutes registers Emby-compatible image endpoints (no auth middleware; optional client cache).
func RegisterImageRoutes(group *gin.RouterGroup, state *AppState) {
	group.GET("/Items/:itemId/Images/:imageType", func(c *gin.Context) { serveImage(c, state) })
	group.GET("/Items/:itemId/Images/:imageType/:imageIndex", func(c *gin.Context) { serveImage(c, state) })
}

func serveImage(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	itemID := c.Param("itemId")
	imageType := c.Param("imageType")
	imageIndex := c.Param("imageIndex")

	uid, err := models.ResolveToUUID(ctx, state.DB, itemID)
	if err != nil || uid == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid item id"})
		return
	}

	var primaryPath, backdropPath *string
	var itemType string
	var castImageURL string
	err = state.DB.QueryRow(ctx,
		`SELECT primary_image_path, backdrop_image_path, type FROM items WHERE id = $1::uuid`,
		*uid).Scan(&primaryPath, &backdropPath, &itemType)
	if err != nil {
		var libImgPath *string
		lerr := state.DB.QueryRow(ctx, "SELECT primary_image_path FROM libraries WHERE id = $1::uuid", *uid).Scan(&libImgPath)
		if lerr != nil || libImgPath == nil || *libImgPath == "" {
			// Rust compatibility: when itemId is a cast_members.id, serve the actor headshot.
			// Many Emby clients request GET /Items/{personId}/Images/Primary where personId
			// is cast_members.id (not in items table).
			var imgURL *string
			state.DB.QueryRow(ctx,
				"SELECT image_url FROM cast_members WHERE id = $1::uuid AND image_url IS NOT NULL LIMIT 1",
				*uid).Scan(&imgURL)
			if imgURL == nil || *imgURL == "" {
				c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
				return
			}
			castImageURL = *imgURL
		} else {
			primaryPath = libImgPath
			itemType = "CollectionFolder"
		}
	}

	tag := c.Query("tag")
	maxW := queryInt(c.Query("maxWidth"))
	if maxW == 0 {
		maxW = queryInt(c.Query("MaxWidth"))
	}
	if maxW == 0 {
		maxW = queryInt(c.Query("Width"))
	}
	if maxW == 0 {
		maxW = queryInt(c.Query("width"))
	}
	maxH := queryInt(c.Query("maxHeight"))
	if maxH == 0 {
		maxH = queryInt(c.Query("MaxHeight"))
	}
	if maxH == 0 {
		maxH = queryInt(c.Query("Height"))
	}
	if maxH == 0 {
		maxH = queryInt(c.Query("height"))
	}
	quality := queryIntDefault(c.Query("quality"), 90)
	if quality < 1 {
		quality = 90
	}
	if quality > 100 {
		quality = 100
	}
	outputFormat := strings.ToLower(strings.TrimSpace(c.Query("Format")))
	if outputFormat == "" {
		outputFormat = strings.ToLower(strings.TrimSpace(c.Query("format")))
	}

	var sourcePath string
	var sourceIsURL bool

	// If this is a cast_member lookup (actor headshot), use the image_url directly
	if castImageURL != "" {
		sourcePath = castImageURL
		sourceIsURL = strings.HasPrefix(strings.ToLower(castImageURL), "http://") ||
			strings.HasPrefix(strings.ToLower(castImageURL), "https://")
	}

	if sourcePath == "" && tag != "" {
		var imgURL *string
		err = state.DB.QueryRow(ctx,
			`SELECT image_url FROM cast_members WHERE id = $1::uuid AND item_id = $2::uuid`,
			tag, *uid).Scan(&imgURL)
		if err == nil && imgURL != nil && *imgURL != "" {
			sourcePath = *imgURL
			sourceIsURL = strings.HasPrefix(strings.ToLower(sourcePath), "http://") ||
				strings.HasPrefix(strings.ToLower(sourcePath), "https://")
		}
	}

	if sourcePath == "" && itemType == "Person" {
		var imgURL *string
		err = state.DB.QueryRow(ctx,
			`SELECT image_url FROM cast_members WHERE id = $1::uuid`,
			*uid).Scan(&imgURL)
		if err == nil && imgURL != nil && *imgURL != "" {
			sourcePath = *imgURL
			sourceIsURL = strings.HasPrefix(strings.ToLower(sourcePath), "http://") ||
				strings.HasPrefix(strings.ToLower(sourcePath), "https://")
		}
	}

	if sourcePath == "" {
		switch imageType {
		case "Primary", "Thumb":
			if primaryPath != nil {
				sourcePath = *primaryPath
			}
		case "Backdrop", "Banner":
			if backdropPath != nil {
				sourcePath = *backdropPath
			}
		default:
			c.JSON(http.StatusBadRequest, gin.H{"message": "Unsupported image type"})
			return
		}
		sourceIsURL = strings.HasPrefix(strings.ToLower(sourcePath), "http://") ||
			strings.HasPrefix(strings.ToLower(sourcePath), "https://")
	}

	if (sourcePath == "" || (!sourceIsURL && !fileExists(sourcePath))) && (itemType == "Episode" || itemType == "Season") {
		var seriesID *string
		state.DB.QueryRow(ctx, "SELECT COALESCE(series_id::text, parent_id::text) FROM items WHERE id = $1::uuid", *uid).Scan(&seriesID)
		if seriesID != nil && *seriesID != "" {
			switch imageType {
			case "Primary", "Thumb":
				state.DB.QueryRow(ctx, "SELECT primary_image_path FROM items WHERE id = $1::uuid", *seriesID).Scan(&sourcePath)
			case "Backdrop", "Banner":
				state.DB.QueryRow(ctx, "SELECT backdrop_image_path FROM items WHERE id = $1::uuid", *seriesID).Scan(&sourcePath)
			}
			sourceIsURL = strings.HasPrefix(strings.ToLower(sourcePath), "http://") ||
				strings.HasPrefix(strings.ToLower(sourcePath), "https://")
		}
	}

	// Rust compatibility: if no image found from item, check cast_members by UUID
	// (handles case where item exists but has no image, and itemId happens to also be a cast_member)
	if sourcePath == "" || (!sourceIsURL && !fileExists(sourcePath)) {
		var castURL *string
		state.DB.QueryRow(ctx,
			"SELECT image_url FROM cast_members WHERE id = $1::uuid AND image_url IS NOT NULL LIMIT 1",
			*uid).Scan(&castURL)
		if castURL != nil && *castURL != "" {
			sourcePath = *castURL
			sourceIsURL = strings.HasPrefix(strings.ToLower(sourcePath), "http://") ||
				strings.HasPrefix(strings.ToLower(sourcePath), "https://")
		}
	}

	if sourcePath == "" || (!sourceIsURL && !fileExists(sourcePath)) {
		state.DB.QueryRow(ctx, "SELECT primary_image_path FROM libraries WHERE id = $1::uuid", *uid).Scan(&sourcePath)
		if sourcePath != "" {
			sourceIsURL = strings.HasPrefix(strings.ToLower(sourcePath), "http://") ||
				strings.HasPrefix(strings.ToLower(sourcePath), "https://")
		}
	}

	if sourcePath == "" {
		c.JSON(http.StatusNotFound, gin.H{"message": "No image"})
		return
	}

	_ = imageIndex // reserved for multi-backdrop; single asset per type in DB today

	imageSemaphore <- struct{}{}
	defer func() { <-imageSemaphore }()

	client := state.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	localPath, err := materializeImageSource(state.Config, client, *uid, imageType, tag, sourcePath, sourceIsURL)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}

	ext := ".jpg"
	encFmt := imaging.JPEG
	switch outputFormat {
	case "png":
		ext = ".png"
		encFmt = imaging.PNG
	case "webp":
		ext = ".jpg"
		encFmt = imaging.JPEG
	}

	outPath := localPath
	if maxW > 0 || maxH > 0 {
		cacheName := fmt.Sprintf("%s_%s_%dx%d%s", *uid, imageType, maxW, maxH, ext)
		if tag != "" {
			cacheName = fmt.Sprintf("%s_%s_%s_%dx%d%s", *uid, imageType, tag, maxW, maxH, ext)
		}
		outPath = filepath.Join(state.Config.CacheDir, cacheName)
		if err := resizeImage(localPath, outPath, maxW, maxH, quality, encFmt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}

	c.Header("Cache-Control", "public, max-age=31536000")
	c.File(outPath)
}

func materializeImageSource(cfg *config.AppConfig, client *http.Client, itemUUID, imageType, tag, source string, isURL bool) (string, error) {
	if !isURL {
		if _, err := os.Stat(source); err != nil {
			return "", err
		}
		return source, nil
	}

	name := fmt.Sprintf("dl_%s_%s", itemUUID, imageType)
	if tag != "" {
		name = fmt.Sprintf("dl_%s_%s_%s", itemUUID, imageType, tag)
	}
	dest := filepath.Join(cfg.CacheDir, name+urlHash(source)+".img")
	if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
		return dest, nil
	}

	resp, err := client.Get(source)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: %s", resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, resp.Body)
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	return dest, nil
}

func urlHash(s string) string {
	h := uint32(2166136261)
	for i := 0; i < len(s) && i < 200; i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return fmt.Sprintf("%08x", h)
}

func resizeImage(srcPath, dstPath string, maxW, maxH, quality int, format imaging.Format) error {
	srcImg, err := imaging.Open(srcPath)
	if err != nil {
		return err
	}
	bounds := srcImg.Bounds()
	sw, sh := bounds.Dx(), bounds.Dy()

	var out image.Image
	switch {
	case maxW > 0 && maxH > 0:
		out = imaging.Fit(srcImg, maxW, maxH, imaging.Lanczos)
	case maxW > 0:
		out = imaging.Resize(srcImg, maxW, 0, imaging.Lanczos)
	case maxH > 0:
		out = imaging.Resize(srcImg, 0, maxH, imaging.Lanczos)
	default:
		return copyFile(srcPath, dstPath)
	}

	ob := out.Bounds()
	if ob.Dx() >= sw && ob.Dy() >= sh {
		return copyFile(srcPath, dstPath)
	}

	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return err
	}
	f, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer f.Close()
	opts := []imaging.EncodeOption{}
	if format == imaging.JPEG {
		opts = append(opts, imaging.JPEGQuality(quality))
	}
	return imaging.Encode(f, out, format, opts...)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func queryInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func queryIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

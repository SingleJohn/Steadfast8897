package handlers

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/gin-gonic/gin"
	_ "golang.org/x/image/webp"

	"fyms/internal/assets"
	"fyms/internal/config"
	"fyms/internal/models"
)

var imageSemaphore = make(chan struct{}, 10)

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func sanitizeImagePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	base := filepath.Base(path)
	if strings.HasPrefix(base, "._") {
		cleanBase := strings.TrimPrefix(base, "._")
		candidate := filepath.Join(filepath.Dir(path), cleanBase)
		if fileExists(candidate) {
			return candidate
		}
	}
	return path
}

// RegisterImageRoutes registers Emby-compatible image endpoints (no auth middleware; optional client cache).
func RegisterImageRoutes(group *gin.RouterGroup, state *AppState) {
	group.GET("/Items/:itemId/Images/:imageType", func(c *gin.Context) { serveImage(c, state) })
	group.GET("/Items/:itemId/Images/:imageType/:imageIndex", func(c *gin.Context) { serveImage(c, state) })
	group.GET("/Library/Platforms/Logo", func(c *gin.Context) {
		name := c.Query("name")
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "name required"})
			return
		}
		servePlatformLogoRaw(c, state, name)
	})
}

func servePlatformLogoRaw(c *gin.Context, state *AppState, platformName string) {
	logoFile := models.PlatformLogoFile(platformName)
	if logoFile == "" {
		c.JSON(http.StatusNotFound, gin.H{"message": "No platform logo"})
		return
	}
	data, err := assets.ReadPlatformLogo(logoFile)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Logo file not found"})
		return
	}
	c.Header("Cache-Control", "public, max-age=31536000")
	c.Data(http.StatusOK, "image/png", data)
}

func servePlatformLogo(c *gin.Context, state *AppState, platformName string) {
	logoFile := models.PlatformLogoFile(platformName)
	if logoFile == "" {
		c.JSON(http.StatusNotFound, gin.H{"message": "No platform logo"})
		return
	}

	cacheKey := fmt.Sprintf("platform_poster_v2_960x540_%s.jpg", strings.ReplaceAll(strings.ToLower(platformName), " ", "_"))
	cachePath := filepath.Join(state.Config.CacheDir, cacheKey)

	if st, err := os.Stat(cachePath); err == nil && st.Size() > 0 {
		c.Header("Cache-Control", "public, max-age=31536000")
		c.File(cachePath)
		return
	}

	logoData, err := assets.ReadPlatformLogo(logoFile)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Logo file not found"})
		return
	}

	logoImg, _, err := image.Decode(bytes.NewReader(logoData))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to decode logo"})
		return
	}

	posterW, posterH := 960, 540
	poster := image.NewRGBA(image.Rect(0, 0, posterW, posterH))
	grad := models.PlatformGradient(platformName)
	drawGradient135(poster, grad)

	logoSize := int(float64(posterH) * 0.55)
	resizedLogo := imaging.Fit(logoImg, logoSize, logoSize, imaging.Lanczos)
	lb := resizedLogo.Bounds()
	offsetX := (posterW - lb.Dx()) / 2
	offsetY := (posterH - lb.Dy()) / 2
	draw.Draw(poster, image.Rect(offsetX, offsetY, offsetX+lb.Dx(), offsetY+lb.Dy()),
		resizedLogo, lb.Min, draw.Over)

	os.MkdirAll(filepath.Dir(cachePath), 0755)
	if f, err := os.Create(cachePath); err == nil {
		imaging.Encode(f, poster, imaging.JPEG, imaging.JPEGQuality(92))
		f.Close()
	}

	c.Header("Cache-Control", "public, max-age=31536000")
	c.File(cachePath)
}

func drawGradient135(img *image.RGBA, colors models.GradientColor) {
	b := img.Bounds()
	w, h := float64(b.Dx()), float64(b.Dy())
	diag := w*0.7071 + h*0.7071

	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			t := (float64(x)*0.7071 + float64(y)*0.7071) / diag
			var r, g, bl uint8
			if t < 0.4 {
				f := t / 0.4
				r = lerpU8(colors[0][0], colors[1][0], f)
				g = lerpU8(colors[0][1], colors[1][1], f)
				bl = lerpU8(colors[0][2], colors[1][2], f)
			} else {
				f := (t - 0.4) / 0.6
				r = lerpU8(colors[1][0], colors[2][0], f)
				g = lerpU8(colors[1][1], colors[2][1], f)
				bl = lerpU8(colors[1][2], colors[2][2], f)
			}
			img.SetRGBA(x, y, color.RGBA{r, g, bl, 255})
		}
	}
}

func lerpU8(a, b uint8, t float64) uint8 {
	return uint8(float64(a)*(1-t) + float64(b)*t)
}

func serveImage(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	itemID := c.Param("itemId")
	imageType := c.Param("imageType")
	imageIndex := c.Param("imageIndex")

	if platformName, ok := models.IsPlatformVirtualID(ctx, state.DB, itemID); ok {
		servePlatformLogo(c, state, platformName)
		return
	}

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
				sourcePath = sanitizeImagePath(*primaryPath)
			}
		case "Backdrop", "Banner":
			if backdropPath != nil {
				sourcePath = sanitizeImagePath(*backdropPath)
			}
		default:
			c.JSON(http.StatusBadRequest, gin.H{"message": "Unsupported image type"})
			return
		}
		sourceIsURL = strings.HasPrefix(strings.ToLower(sourcePath), "http://") ||
			strings.HasPrefix(strings.ToLower(sourcePath), "https://")
	}

	// Fallback: if item has no image, try merged secondaries (merged_to_id → this item)
	if (sourcePath == "" || (!sourceIsURL && !fileExists(sourcePath))) && (itemType == "Movie" || itemType == "Series") {
		var mergedPrimary, mergedBackdrop *string
		state.DB.QueryRow(ctx,
			`SELECT primary_image_path, backdrop_image_path
			 FROM items WHERE merged_to_id = $1::uuid
			   AND primary_image_path IS NOT NULL
			 LIMIT 1`, *uid).Scan(&mergedPrimary, &mergedBackdrop)
		if mergedPrimary == nil || *mergedPrimary == "" {
			// Also check if THIS item is a secondary — use primary's image
			state.DB.QueryRow(ctx,
				`SELECT p.primary_image_path, p.backdrop_image_path
				 FROM items s JOIN items p ON p.id = s.merged_to_id
				 WHERE s.id = $1::uuid AND p.primary_image_path IS NOT NULL`, *uid).Scan(&mergedPrimary, &mergedBackdrop)
		}
		switch imageType {
		case "Primary", "Thumb":
			if mergedPrimary != nil && *mergedPrimary != "" {
				sourcePath = sanitizeImagePath(*mergedPrimary)
				sourceIsURL = strings.HasPrefix(strings.ToLower(sourcePath), "http://") ||
					strings.HasPrefix(strings.ToLower(sourcePath), "https://")
			}
		case "Backdrop", "Banner":
			if mergedBackdrop != nil && *mergedBackdrop != "" {
				sourcePath = sanitizeImagePath(*mergedBackdrop)
				sourceIsURL = strings.HasPrefix(strings.ToLower(sourcePath), "http://") ||
					strings.HasPrefix(strings.ToLower(sourcePath), "https://")
			}
		}
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
			sourcePath = sanitizeImagePath(sourcePath)
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
		sourcePath = sanitizeImagePath(sourcePath)
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
		if st, serr := os.Stat(outPath); serr == nil && st.Size() > 0 {
			// cached resize exists
		} else if err := resizeImage(localPath, outPath, maxW, maxH, quality, encFmt); err != nil {
			slog.Warn("[Image] resize failed, serving original", "path", localPath, "error", err)
			outPath = localPath
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

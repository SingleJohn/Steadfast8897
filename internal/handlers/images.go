package handlers

import (
	"bytes"
	"crypto/sha1"
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
	cachePath := state.ImageCache.ResizedPath(cacheKey)

	if st, err := os.Stat(cachePath); err == nil && st.Size() > 0 {
		state.ImageCache.Touch(cachePath)
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

// servePlatformCover 出虚拟库生成的封面图(本地文件),支持按 maxWidth/maxHeight 缩放缓存。
func servePlatformCover(c *gin.Context, state *AppState, coverPath string) {
	maxW := queryInt(c.Query("maxWidth"))
	if maxW == 0 {
		maxW = queryInt(c.Query("MaxWidth"))
	}
	if maxW == 0 {
		maxW = queryInt(c.Query("Width"))
	}
	maxH := queryInt(c.Query("maxHeight"))
	if maxH == 0 {
		maxH = queryInt(c.Query("MaxHeight"))
	}
	if maxH == 0 {
		maxH = queryInt(c.Query("Height"))
	}
	quality := queryIntDefault(c.Query("quality"), 90)
	if quality < 1 || quality > 100 {
		quality = 90
	}

	c.Header("Cache-Control", "public, max-age=31536000")
	if maxW <= 0 && maxH <= 0 {
		c.File(coverPath)
		return
	}

	st, err := os.Stat(coverPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "cover not found"})
		return
	}
	cacheName := fmt.Sprintf("platcover_%x_%d_%dx%d_q%d.jpg",
		sha1.Sum([]byte(coverPath)), st.ModTime().Unix(), maxW, maxH, quality)
	outPath := state.ImageCache.ResizedPath(cacheName)
	if rst, serr := os.Stat(outPath); serr == nil && rst.Size() > 0 {
		state.ImageCache.Touch(outPath)
	} else if err := resizeImage(coverPath, outPath, maxW, maxH, quality, imaging.JPEG); err != nil {
		slog.Warn("[Image] platform cover resize failed, serving original", "path", coverPath, "error", err)
		c.File(coverPath)
		return
	}
	c.File(outPath)
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
	imageType := normalizeImageType(c.Param("imageType"))
	imageIndex := c.Param("imageIndex")

	if p, ok := models.ResolvePlatformVirtualID(ctx, state.DB, itemID); ok {
		// 优先用生成的封面图;没有则回退已知平台的内置 logo
		if p.CoverImagePath != nil && *p.CoverImagePath != "" && fileExists(*p.CoverImagePath) {
			servePlatformCover(c, state, *p.CoverImagePath)
			return
		}
		servePlatformLogo(c, state, p.PlatformName)
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
	itemInfo, err := state.Repo.ImageLookup.GetItemImageInfo(ctx, *uid)
	if err == nil && itemInfo != nil {
		primaryPath = itemInfo.PrimaryPath
		backdropPath = itemInfo.BackdropPath
		itemType = itemInfo.Type
	} else {
		libImgPath, lerr := state.Repo.ImageLookup.GetLibraryPrimaryImagePath(ctx, *uid)
		if lerr != nil || libImgPath == nil || *libImgPath == "" {
			// 演员图:itemId 可能是全局 persons.id(新)或 cast_members.id(旧/兜底)。
			// Emby 客户端请求 GET /Items/{personId}/Images/{Primary|Backdrop},personId 不在 items 表。
			if strings.EqualFold(imageType, "Backdrop") {
				if img, ok := models.GetPersonBackdropPath(ctx, state.DB, *uid); ok {
					castImageURL = img
				} else {
					c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
					return
				}
			} else if img, ok := models.GetPersonImagePath(ctx, state.DB, *uid); ok {
				castImageURL = img
			} else {
				imgURL, _ := state.Repo.ImageLookup.GetCastImageURL(ctx, *uid)
				if imgURL == nil || *imgURL == "" {
					c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
					return
				}
				castImageURL = *imgURL
			}
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
		imgURL, err := state.Repo.ImageLookup.GetCastImageURLByTagAndItem(ctx, tag, *uid)
		if err == nil && imgURL != nil && *imgURL != "" {
			sourcePath = *imgURL
			sourceIsURL = strings.HasPrefix(strings.ToLower(sourcePath), "http://") ||
				strings.HasPrefix(strings.ToLower(sourcePath), "https://")
		}
	}

	if sourcePath == "" && itemType == "Person" {
		imgURL, err := state.Repo.ImageLookup.GetCastImageURL(ctx, *uid)
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
			// extrafanart 多图:Backdrop/{index} index>=1 走 item_images;index 空/0 用主 backdrop。
			if idx := queryInt(imageIndex); idx > 0 && imageType == "Backdrop" {
				extraPath, _ := state.Repo.ImageLookup.GetItemExtraImagePath(ctx, *uid, int32(idx))
				if extraPath != nil && *extraPath != "" {
					sourcePath = sanitizeImagePath(*extraPath)
				}
			}
			if sourcePath == "" && backdropPath != nil {
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
		if paths, err := state.Repo.ImageLookup.GetMergedSecondaryImagePaths(ctx, *uid); err == nil && paths != nil {
			mergedPrimary = paths.PrimaryPath
			mergedBackdrop = paths.BackdropPath
		}
		if mergedPrimary == nil || *mergedPrimary == "" {
			// Also check if THIS item is a secondary — use primary's image
			if paths, err := state.Repo.ImageLookup.GetMergedPrimaryImagePaths(ctx, *uid); err == nil && paths != nil {
				mergedPrimary = paths.PrimaryPath
				mergedBackdrop = paths.BackdropPath
			}
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
		seriesID, _ := state.Repo.ImageLookup.GetEpisodeSeriesImageParentID(ctx, *uid)
		if seriesID != nil && *seriesID != "" {
			switch imageType {
			case "Primary", "Thumb":
				if path, err := state.Repo.ImageLookup.GetItemPrimaryImagePath(ctx, *seriesID); err == nil && path != nil {
					sourcePath = *path
				}
			case "Backdrop", "Banner":
				if path, err := state.Repo.ImageLookup.GetItemBackdropImagePath(ctx, *seriesID); err == nil && path != nil {
					sourcePath = *path
				}
			}
			sourcePath = sanitizeImagePath(sourcePath)
			sourceIsURL = strings.HasPrefix(strings.ToLower(sourcePath), "http://") ||
				strings.HasPrefix(strings.ToLower(sourcePath), "https://")
		}
	}

	// Rust compatibility: if no image found from item, check cast_members by UUID
	// (handles case where item exists but has no image, and itemId happens to also be a cast_member)
	if sourcePath == "" || (!sourceIsURL && !fileExists(sourcePath)) {
		castURL, _ := state.Repo.ImageLookup.GetCastImageURL(ctx, *uid)
		if castURL != nil && *castURL != "" {
			sourcePath = *castURL
			sourceIsURL = strings.HasPrefix(strings.ToLower(sourcePath), "http://") ||
				strings.HasPrefix(strings.ToLower(sourcePath), "https://")
		}
	}

	if sourcePath == "" || (!sourceIsURL && !fileExists(sourcePath)) {
		if path, err := state.Repo.ImageLookup.GetLibraryPrimaryImagePath(ctx, *uid); err == nil && path != nil {
			sourcePath = *path
		}
		sourcePath = sanitizeImagePath(sourcePath)
		if sourcePath != "" {
			sourceIsURL = strings.HasPrefix(strings.ToLower(sourcePath), "http://") ||
				strings.HasPrefix(strings.ToLower(sourcePath), "https://")
		}
	}

	// Emby 式跨类型兜底:请求 Backdrop/Banner 但全程未找到时,回落到本条目自身的 Primary 图
	// (剧集分集的本地封面 <basename>-thumb.jpg 存在 primary_image_path),再退到所属剧集的 Primary。
	// Emby 客户端/通知工具在缺 Backdrop 时会自动用 thumb,FYMS 之前只在同类型内回落,导致直接 404。
	if (sourcePath == "" || (!sourceIsURL && !fileExists(sourcePath))) && (imageType == "Backdrop" || imageType == "Banner") {
		if primaryPath != nil && *primaryPath != "" {
			sourcePath = sanitizeImagePath(*primaryPath)
			sourceIsURL = strings.HasPrefix(strings.ToLower(sourcePath), "http://") ||
				strings.HasPrefix(strings.ToLower(sourcePath), "https://")
		}
		if (sourcePath == "" || (!sourceIsURL && !fileExists(sourcePath))) && (itemType == "Episode" || itemType == "Season") {
			if seriesID, _ := state.Repo.ImageLookup.GetEpisodeSeriesImageParentID(ctx, *uid); seriesID != nil && *seriesID != "" {
				if path, err := state.Repo.ImageLookup.GetItemPrimaryImagePath(ctx, *seriesID); err == nil && path != nil && *path != "" {
					sourcePath = sanitizeImagePath(*path)
					sourceIsURL = strings.HasPrefix(strings.ToLower(sourcePath), "http://") ||
						strings.HasPrefix(strings.ToLower(sourcePath), "https://")
				}
			}
		}
	}

	if sourcePath == "" {
		c.JSON(http.StatusNotFound, gin.H{"message": "No image"})
		return
	}

	imageSemaphore <- struct{}{}
	defer func() { <-imageSemaphore }()

	localPath, _, err := state.ImageCache.Materialize(sourcePath, sourceIsURL)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}

	encFmt := imaging.JPEG
	contentType := "image/jpeg"
	switch outputFormat {
	case "png":
		encFmt = imaging.PNG
		contentType = "image/png"
	case "webp":
		encFmt = imaging.JPEG
		contentType = "image/jpeg"
	}

	if maxW > 0 || maxH > 0 {
		wrote, err := writeResizedImage(c.Writer, localPath, maxW, maxH, quality, encFmt, func() {
			c.Header("Cache-Control", "public, max-age=31536000")
			c.Header("Content-Type", contentType)
		})
		if err != nil {
			slog.Warn("[Image] resize failed, serving original", "path", localPath, "error", err)
			c.File(localPath)
		} else if !wrote {
			c.Header("Cache-Control", "public, max-age=31536000")
			c.File(localPath)
		}
		return
	}

	state.ImageCache.Touch(localPath)
	c.Header("Cache-Control", "public, max-age=31536000")
	c.File(localPath)
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

func writeResizedImage(w io.Writer, srcPath string, maxW, maxH, quality int, format imaging.Format, beforeWrite func()) (bool, error) {
	srcImg, err := imaging.Open(srcPath)
	if err != nil {
		return false, err
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
		return false, nil
	}

	ob := out.Bounds()
	if ob.Dx() >= sw && ob.Dy() >= sh {
		return false, nil
	}

	opts := []imaging.EncodeOption{}
	if format == imaging.JPEG {
		opts = append(opts, imaging.JPEGQuality(quality))
	}
	if beforeWrite != nil {
		beforeWrite()
	}
	return true, imaging.Encode(w, out, format, opts...)
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

func normalizeImageType(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "primary", "poster", "thumb":
		if strings.EqualFold(strings.TrimSpace(s), "thumb") {
			return "Thumb"
		}
		return "Primary"
	case "backdrop", "backdrops", "fanart":
		return "Backdrop"
	case "banner":
		return "Banner"
	default:
		return strings.TrimSpace(s)
	}
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

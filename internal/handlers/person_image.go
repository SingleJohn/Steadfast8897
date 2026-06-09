package handlers

import (
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"fyms/internal/models"
)

// personsImageSubdir 是 person 头像的存储子目录(data/metadata/persons)。
const personsImageSubdir = "persons"

// handlePersonImageUpload 处理 person 头像上传。uploadImage 在 itemId 命中 persons
// 表时分流到此。写 persons.image_path 并锁定 —— 全库同名条目随之生效。
// 兼容 Emby/第三方客户端:body 可能是 multipart file、原始二进制或 base64 文本。
func handlePersonImageUpload(c *gin.Context, state *AppState, personID string) {
	ctx := c.Request.Context()

	var data []byte
	if file, err := c.FormFile("file"); err == nil {
		src, oerr := file.Open()
		if oerr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": oerr.Error()})
			return
		}
		defer src.Close()
		data, _ = io.ReadAll(src)
	} else {
		raw, rerr := io.ReadAll(c.Request.Body)
		if rerr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": rerr.Error()})
			return
		}
		data = decodeMaybeBase64(raw)
	}
	if len(data) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "empty image body"})
		return
	}

	ext := personImageStorageExt(data)
	dir := filepath.Join(state.Config.DataDir, "metadata", personsImageSubdir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	// 文件名用 personID,重传覆盖,稳定可预测。
	fpath := filepath.Join(dir, personID+ext)
	if err := os.WriteFile(fpath, data, 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if err := models.SetPersonImage(ctx, state.DB, personID, fpath, true); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// handlePersonImageDelete 清除 person 头像(磁盘文件 + 解锁)。
func handlePersonImageDelete(c *gin.Context, state *AppState, personID string) {
	ctx := c.Request.Context()
	if img, ok := models.GetPersonImagePath(ctx, state.DB, personID); ok {
		// 只删 data/metadata/persons 下我们自己写的文件,绝不碰 NFO/挂载盘里的原图。
		if isUnderPersonsDir(state, img) {
			_ = os.Remove(img)
		}
	}
	if err := models.ClearPersonImage(ctx, state.DB, personID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// decodeMaybeBase64 兼容客户端把图片当 base64 文本 POST 的情况。
// 已是二进制图片则原样返回;否则剥离可能的 data URI 前缀后 base64 解码。
func decodeMaybeBase64(raw []byte) []byte {
	if imageExtFromMagic(raw) != "" {
		return raw
	}
	s := strings.TrimSpace(string(raw))
	if i := strings.Index(s, "base64,"); i >= 0 {
		s = s[i+len("base64,"):]
	}
	if decoded, err := base64.StdEncoding.DecodeString(s); err == nil && len(decoded) > 0 {
		return decoded
	}
	return raw
}

// imageExtFromMagic 按魔数判断图片类型,返回扩展名;非已知图片返回空串。
func imageExtFromMagic(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return ".jpg"
	case len(b) >= 8 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G':
		return ".png"
	case len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return ".webp"
	}
	return ""
}

// personImageStorageExt 给存储用的扩展名,未知类型兜底 .jpg(imaging 按内容嗅探解码)。
func personImageStorageExt(b []byte) string {
	if e := imageExtFromMagic(b); e != "" {
		return e
	}
	return ".jpg"
}

func isUnderPersonsDir(state *AppState, p string) bool {
	base, err := filepath.Abs(filepath.Join(state.Config.DataDir, "metadata", personsImageSubdir))
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	return strings.HasPrefix(abs, base+string(filepath.Separator)) || abs == base
}

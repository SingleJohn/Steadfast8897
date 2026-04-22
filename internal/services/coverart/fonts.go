package coverart

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

//go:embed assets/fonts/*
var embeddedFontsFS embed.FS

const (
	embeddedFontsDir = "assets/fonts"
	externalFontsDir = "data/fonts"
)

var (
	fontOnce   sync.Once
	cachedFont *opentype.Font
	fontErr    error
)

// loadTitleFont 返回全局共享的标题字体(Font 对象线程安全)。
// 查找顺序:
//  1. 嵌入的 assets/fonts/ 下任意 .otf/.ttf
//  2. 外部 data/fonts/ 下任意 .otf/.ttf
//  3. 都没有 → ErrFontMissing
func loadTitleFont() (*opentype.Font, error) {
	fontOnce.Do(func() {
		if data, ok := readEmbeddedFont(); ok {
			f, err := opentype.Parse(data)
			if err == nil {
				cachedFont = f
				return
			}
			fontErr = err
			return
		}
		if data, ok := readExternalFont(); ok {
			f, err := opentype.Parse(data)
			if err == nil {
				cachedFont = f
				return
			}
			fontErr = err
			return
		}
		fontErr = ErrFontMissing
	})
	return cachedFont, fontErr
}

// makeFace 从缓存字体构造指定字号的 face;face 生命周期交给调用方管理。
func makeFace(size float64) (font.Face, error) {
	f, err := loadTitleFont()
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(f, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
}

func readEmbeddedFont() ([]byte, bool) {
	entries, err := fs.ReadDir(embeddedFontsFS, embeddedFontsDir)
	if err != nil {
		return nil, false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".otf" && ext != ".ttf" {
			continue
		}
		data, err := fs.ReadFile(embeddedFontsFS, embeddedFontsDir+"/"+e.Name())
		if err == nil && len(data) > 0 {
			return data, true
		}
	}
	return nil, false
}

func readExternalFont() ([]byte, bool) {
	entries, err := os.ReadDir(externalFontsDir)
	if err != nil {
		return nil, false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".otf" && ext != ".ttf" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(externalFontsDir, e.Name()))
		if err == nil && len(data) > 0 {
			return data, true
		}
	}
	return nil, false
}

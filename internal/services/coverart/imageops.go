package coverart

import (
	"image"
	"os"

	"github.com/disintegration/imaging"
)

// loadImage 从文件加载图像,支持 JPEG/PNG/WEBP(通过 imaging 透明处理)。
func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return imaging.Decode(f, imaging.AutoOrientation(true))
}

// coverFit 把 src 按 CenterCrop 填满 w×h 目标尺寸,Lanczos 采样。
func coverFit(src image.Image, w, h int) image.Image {
	return imaging.Fill(src, w, h, imaging.Center, imaging.Lanczos)
}

// placeholderTile 在无法加载海报时用的占位灰块。
func placeholderTile(w, h int) image.Image {
	img := imaging.New(w, h, fallbackDominant)
	return img
}

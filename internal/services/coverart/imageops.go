package coverart

import (
	"image"
	"image/color"
	"image/draw"
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

// placeholderTile 在无法加载海报时用的占位灰块(保持 2:3 比例的灰)。
func placeholderTile(w, h int) image.Image {
	return imaging.New(w, h, color.NRGBA{0x55, 0x5a, 0x62, 0xff})
}

// roundCorners 给 src 的四角做圆角遮罩,超出圆角部分 alpha=0。
// 输入尺寸不变,输出 NRGBA 便于后续 alpha 合成。
func roundCorners(src image.Image, radius int) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if radius <= 0 {
		out := image.NewNRGBA(image.Rect(0, 0, w, h))
		draw.Draw(out, out.Bounds(), src, b.Min, draw.Src)
		return out
	}
	if radius*2 > w {
		radius = w / 2
	}
	if radius*2 > h {
		radius = h / 2
	}

	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), src, b.Min, draw.Src)

	rr := radius * radius
	// 四角:离对应角圆心距离大于 radius 的像素 alpha 置 0
	corners := [4][2]int{
		{radius, radius},         // 左上圆心
		{w - radius - 1, radius}, // 右上
		{radius, h - radius - 1}, // 左下
		{w - radius - 1, h - radius - 1},
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// 只检查四个角的 radius×radius 子区域
			var inCorner bool
			var cx, cy int
			switch {
			case x < radius && y < radius:
				inCorner, cx, cy = true, corners[0][0], corners[0][1]
			case x >= w-radius && y < radius:
				inCorner, cx, cy = true, corners[1][0], corners[1][1]
			case x < radius && y >= h-radius:
				inCorner, cx, cy = true, corners[2][0], corners[2][1]
			case x >= w-radius && y >= h-radius:
				inCorner, cx, cy = true, corners[3][0], corners[3][1]
			}
			if !inCorner {
				continue
			}
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy > rr {
				out.SetNRGBA(x, y, color.NRGBA{})
			}
		}
	}
	return out
}

// withDropShadow 把 src 放进一个更大的画布,先画出模糊阴影再画 src。
// offsetX/Y 阴影偏移,blurRadius 模糊半径(imaging.Blur 的 sigma),alpha 阴影最大不透明度(0..1)。
// 返回的图比 src 大 padding 像素,调用方自己按其 Bounds 决定贴到底图的位置。
func withDropShadow(src image.Image, offsetX, offsetY int, blurRadius float64, alpha float64) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	pad := int(blurRadius*2) + 4
	if pad < 12 {
		pad = 12
	}
	outW, outH := w+pad*2, h+pad*2
	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))

	// 构造阴影:按 src 的 alpha 生成一张同尺寸的黑色图像
	shadow := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	aByte := uint8(alpha * 255)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			_, _, _, a := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			if a < 0x4000 { // alpha < 0.25 视为透明,不参与阴影
				continue
			}
			sa := uint8(uint32(aByte) * a / 0xffff)
			shadow.SetNRGBA(pad+x+offsetX, pad+y+offsetY, color.NRGBA{0, 0, 0, sa})
		}
	}
	blurred := imaging.Blur(shadow, blurRadius)
	draw.Draw(out, out.Bounds(), blurred, image.Point{}, draw.Over)
	draw.Draw(out, image.Rect(pad, pad, pad+w, pad+h), src, b.Min, draw.Over)
	return out
}

// drawVerticalGradient 在 dst 的整个矩形区域画竖向渐变(顶 colorTop → 底 colorBottom)。
func drawVerticalGradient(dst *image.RGBA, colorTop, colorBottom color.RGBA) {
	b := dst.Bounds()
	w, h := b.Dx(), b.Dy()
	for y := 0; y < h; y++ {
		t := float64(y) / float64(h-1)
		col := lerpColor(colorTop, colorBottom, t)
		for x := 0; x < w; x++ {
			dst.SetRGBA(b.Min.X+x, b.Min.Y+y, col)
		}
	}
}

// drawFilledRect 在 dst 上用单一颜色填充矩形 r。
func drawFilledRect(dst draw.Image, r image.Rectangle, c color.Color) {
	draw.Draw(dst, r, &image.Uniform{C: c}, image.Point{}, draw.Src)
}

package coverart

import (
	"image"
	"image/color"

	"github.com/disintegration/imaging"
)

// fallbackDominant 是主色提取全部过滤后的兜底。
var fallbackDominant = color.RGBA{R: 0x2a, G: 0x2a, B: 0x3a, A: 0xff}

// DominantColor 用轻量直方图投票提取主色:
//  1. 缩略到 100px 宽采样 ~1 万像素以内;
//  2. 逐像素转 HSL,过滤饱和度 S<0.25、亮度 L<0.15 或 L>0.85 的灰/黑/白;
//  3. H 分 24 个 bin(每 15°),选票数最高的 bin,取桶内 RGB 均值返回;
//  4. 全部被过滤则返回 fallbackDominant。
func DominantColor(img image.Image) color.RGBA {
	if img == nil {
		return fallbackDominant
	}
	small := imaging.Resize(img, 100, 0, imaging.Box)
	b := small.Bounds()

	const bins = 24
	type bucket struct {
		count            int
		sumR, sumG, sumB int64
	}
	var hist [bins]bucket

	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bb, _ := small.At(x, y).RGBA()
			r8, g8, b8 := uint8(r>>8), uint8(g>>8), uint8(bb>>8)
			h, s, l := rgbToHSL(r8, g8, b8)
			if s < 0.25 || l < 0.15 || l > 0.85 {
				continue
			}
			idx := int(h/360.0*float64(bins)) % bins
			hist[idx].count++
			hist[idx].sumR += int64(r8)
			hist[idx].sumG += int64(g8)
			hist[idx].sumB += int64(b8)
		}
	}

	bestIdx := -1
	bestCount := 0
	for i := 0; i < bins; i++ {
		if hist[i].count > bestCount {
			bestCount = hist[i].count
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return fallbackDominant
	}
	bk := hist[bestIdx]
	return color.RGBA{
		R: uint8(bk.sumR / int64(bk.count)),
		G: uint8(bk.sumG / int64(bk.count)),
		B: uint8(bk.sumB / int64(bk.count)),
		A: 0xff,
	}
}

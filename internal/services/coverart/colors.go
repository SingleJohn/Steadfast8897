package coverart

import (
	"image/color"
	"math"
)

// rgbToHSL 转到 HSL 空间,H∈[0,360),S/L∈[0,1]。
func rgbToHSL(r, g, b uint8) (h, s, l float64) {
	rf := float64(r) / 255.0
	gf := float64(g) / 255.0
	bf := float64(b) / 255.0

	maxv := math.Max(rf, math.Max(gf, bf))
	minv := math.Min(rf, math.Min(gf, bf))
	l = (maxv + minv) / 2.0

	if maxv == minv {
		return 0, 0, l
	}
	d := maxv - minv
	if l > 0.5 {
		s = d / (2.0 - maxv - minv)
	} else {
		s = d / (maxv + minv)
	}
	switch maxv {
	case rf:
		h = (gf - bf) / d
		if gf < bf {
			h += 6.0
		}
	case gf:
		h = (bf-rf)/d + 2.0
	default:
		h = (rf-gf)/d + 4.0
	}
	h *= 60.0
	return
}

func hue2rgb(p, q, t float64) float64 {
	if t < 0 {
		t += 1
	}
	if t > 1 {
		t -= 1
	}
	switch {
	case t < 1.0/6.0:
		return p + (q-p)*6.0*t
	case t < 0.5:
		return q
	case t < 2.0/3.0:
		return p + (q-p)*(2.0/3.0-t)*6.0
	}
	return p
}

// hslToRGB h∈[0,360),s/l∈[0,1]。
func hslToRGB(h, s, l float64) color.RGBA {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	hh := h / 360.0
	if s == 0 {
		v := uint8(math.Round(l * 255))
		return color.RGBA{v, v, v, 255}
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	r := hue2rgb(p, q, hh+1.0/3.0)
	g := hue2rgb(p, q, hh)
	b := hue2rgb(p, q, hh-1.0/3.0)
	return color.RGBA{
		R: uint8(math.Round(r * 255)),
		G: uint8(math.Round(g * 255)),
		B: uint8(math.Round(b * 255)),
		A: 255,
	}
}

// shade 把颜色向黑色拉近 factor 倍(factor=-0.35 ≈ 亮度降低 35%)。
func shade(c color.RGBA, factor float64) color.RGBA {
	h, s, l := rgbToHSL(c.R, c.G, c.B)
	l += factor
	if l < 0 {
		l = 0
	} else if l > 1 {
		l = 1
	}
	return hslToRGB(h, s, l)
}

// tint 把颜色向白色拉近 factor 倍(factor=0.15 ≈ 亮度提高 15%)。
func tint(c color.RGBA, factor float64) color.RGBA {
	return shade(c, factor)
}

// softBackground 把主色转成适合做封面大面积背景的柔和淡色。
// 保留色相,把饱和度压低并把亮度拉高,避免主色过饱和造成视觉刺眼。
// 参考 jellyfin-library-poster 的效果:淡绿、淡蓝、淡灰等。
func softBackground(c color.RGBA) color.RGBA {
	h, s, _ := rgbToHSL(c.R, c.G, c.B)
	if s < 0.15 {
		// 主色本身就偏灰,直接给一个中性蓝灰
		return color.RGBA{R: 0x8b, G: 0x9e, B: 0xaa, A: 0xff}
	}
	// S 压到 0.30,L 拉到 0.68 — 靠近柔和 pastel
	newS := 0.30
	newL := 0.68
	return hslToRGB(h, newS, newL)
}

// softBackgroundDarker 用于 softBackground 的底部竖向渐变参考色,比背景略深一点。
func softBackgroundDarker(c color.RGBA) color.RGBA {
	h, s, _ := rgbToHSL(c.R, c.G, c.B)
	if s < 0.15 {
		return color.RGBA{R: 0x6f, G: 0x80, B: 0x8c, A: 0xff}
	}
	return hslToRGB(h, 0.34, 0.54)
}

// lerpColor 在两个颜色间线性插值,t∈[0,1]。
func lerpColor(a, b color.RGBA, t float64) color.RGBA {
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return color.RGBA{
		R: uint8(math.Round(float64(a.R) + (float64(b.R)-float64(a.R))*t)),
		G: uint8(math.Round(float64(a.G) + (float64(b.G)-float64(a.G))*t)),
		B: uint8(math.Round(float64(a.B) + (float64(b.B)-float64(a.B))*t)),
		A: 255,
	}
}

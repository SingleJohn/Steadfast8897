package coverart

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// ninegridStyle 九宫格拼贴风格:
//
//	┌───────────────────────────────┐
//	│ 库名                          │ <- 左侧渐变蒙版压暗,文字白色 + 阴影
//	│ (居中或顶部)     ┌──┬──┬──┐   │
//	│                 │P │P │P │   │
//	│                 ├──┼──┼──┤   │
//	│                 │P │P │P │   │
//	│                 ├──┼──┼──┤   │
//	│                 │P │P │P │   │
//	│                 └──┴──┴──┘   │
//	└───────────────────────────────┘
type ninegridStyle struct{}

func (ninegridStyle) Name() string            { return "ninegrid" }
func (ninegridStyle) Label() string           { return "九宫格拼贴" }
func (ninegridStyle) AspectRatio() (int, int) { return 16, 9 }

func init() {
	Register(ninegridStyle{})
}

// Render 渲染九宫格封面。流程:
//  1. 并发加载 9 张海报(失败的位置用 fallback 灰块)
//  2. 从首张海报提取主色,算出 colorA(深)/colorB(浅)
//  3. 渐变底填满整图(指数 0.7 非线性过渡)
//  4. 右侧 3x3 网格,每格 coverFit 裁到单元尺寸
//  5. 九宫格上叠左黑右透的水平蒙版,让左侧可读
//  6. 左侧绘制库名:单行二分字号,放不下则多行 wrap(封顶 3 行)
//  7. JPEG 编码,quality=88
func (ninegridStyle) Render(ctx context.Context, in Input) (Output, error) {
	w, h := in.OutputWidth, in.OutputHeight
	if w <= 0 {
		w = 1920
	}
	if h <= 0 {
		h = 1080
	}

	posters := loadPostersConcurrent(ctx, in.PosterPaths)

	dominant := DominantColor(posters[0])
	colorA := shade(dominant, -0.35)
	colorB := tint(dominant, +0.15)

	base := image.NewRGBA(image.Rect(0, 0, w, h))
	drawHorizontalGradient(base, colorA, colorB, 0.7)

	if err := ctx.Err(); err != nil {
		return Output{}, err
	}

	// 右侧网格区域:占约 60% 宽度,保留 60px 上下边距
	gridRight := w - 40
	gridTop := 60
	gridBottom := h - 60
	gap := 16
	cols, rowsN := 3, 3
	cellW := 360
	cellH := (gridBottom - gridTop - gap*(rowsN-1)) / rowsN
	if cellH <= 0 {
		cellH = 300
	}
	gridWidth := cellW*cols + gap*(cols-1)
	gridLeft := gridRight - gridWidth

	drawGrid(base, posters, gridLeft, gridTop, cellW, cellH, gap, cols, rowsN)

	if err := ctx.Err(); err != nil {
		return Output{}, err
	}

	// 左侧渐变蒙版:从左 α=0.85 → 右 α=0,压暗九宫格左缘让文字可读
	drawHorizontalAlphaMask(base,
		image.Rect(0, 0, gridLeft+gridWidth/2, h),
		color.RGBA{0, 0, 0, 0}, // 颜色固定黑色
		0.78, 0.0, 0.7)

	// 标题文字左侧区域:左内边距 80,宽度上限 maxTitleW
	titleMaxW := gridLeft - 120
	if titleMaxW < 360 {
		titleMaxW = 360
	}
	if err := drawTitle(base, in.LibraryName, 80, h/2, titleMaxW, h-120); err != nil {
		return Output{}, err
	}

	return Output{
		Image:   base,
		Mime:    "image/jpeg",
		Quality: 88,
	}, nil
}

// loadPostersConcurrent 并发加载最多 9 张海报,失败返回占位。
func loadPostersConcurrent(ctx context.Context, paths []string) []image.Image {
	out := make([]image.Image, 9)
	var wg sync.WaitGroup
	for i := 0; i < 9; i++ {
		idx := i
		var p string
		if idx < len(paths) {
			p = paths[idx]
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ctx.Err() != nil || p == "" {
				out[idx] = placeholderTile(360, 540)
				return
			}
			img, err := loadImage(p)
			if err != nil || img == nil {
				out[idx] = placeholderTile(360, 540)
				return
			}
			out[idx] = img
		}()
	}
	wg.Wait()
	return out
}

// drawHorizontalGradient 从左(colorA)到右(colorB),t=(x/w)^exponent。
func drawHorizontalGradient(dst *image.RGBA, a, b color.RGBA, exponent float64) {
	w := dst.Bounds().Dx()
	h := dst.Bounds().Dy()
	for x := 0; x < w; x++ {
		t := math.Pow(float64(x)/float64(w-1), exponent)
		col := lerpColor(a, b, t)
		for y := 0; y < h; y++ {
			dst.SetRGBA(x, y, col)
		}
	}
}

// drawHorizontalAlphaMask 在 rect 区域叠加水平透明度渐变层:
// 左端 α=startA,右端 α=endA,非线性 exponent。颜色固定 fillColor。
func drawHorizontalAlphaMask(dst *image.RGBA, rect image.Rectangle, fillColor color.RGBA, startA, endA, exponent float64) {
	rw := rect.Dx()
	if rw <= 0 {
		return
	}
	for x := 0; x < rw; x++ {
		t := math.Pow(float64(x)/float64(rw-1), exponent)
		a := startA + (endA-startA)*t
		if a < 0 {
			a = 0
		} else if a > 1 {
			a = 1
		}
		alpha := uint8(math.Round(a * 255))
		c := color.RGBA{R: fillColor.R, G: fillColor.G, B: fillColor.B, A: alpha}
		overlayColumn(dst, rect.Min.X+x, rect.Min.Y, rect.Max.Y, c)
	}
}

// overlayColumn 在 dst 的某一列(x 固定)上做 source-over 合成 c。
func overlayColumn(dst *image.RGBA, x, y0, y1 int, c color.RGBA) {
	if c.A == 0 {
		return
	}
	sr, sg, sb, sa := float64(c.R), float64(c.G), float64(c.B), float64(c.A)/255.0
	for y := y0; y < y1; y++ {
		old := dst.RGBAAt(x, y)
		dr := float64(old.R)*(1-sa) + sr*sa
		dg := float64(old.G)*(1-sa) + sg*sa
		db := float64(old.B)*(1-sa) + sb*sa
		dst.SetRGBA(x, y, color.RGBA{
			R: uint8(dr),
			G: uint8(dg),
			B: uint8(db),
			A: 255,
		})
	}
}

// drawGrid 把 9 张海报按 3 行 3 列 coverFit 到 dst 的 (x0,y0)+(cellW*cols+gap*(cols-1)) 区域。
func drawGrid(dst *image.RGBA, posters []image.Image, x0, y0, cellW, cellH, gap, cols, rowsN int) {
	for r := 0; r < rowsN; r++ {
		for c := 0; c < cols; c++ {
			idx := r*cols + c
			if idx >= len(posters) || posters[idx] == nil {
				continue
			}
			fitted := coverFit(posters[idx], cellW, cellH)
			cx := x0 + c*(cellW+gap)
			cy := y0 + r*(cellH+gap)
			draw.Draw(dst, image.Rect(cx, cy, cx+cellW, cy+cellH), fitted, image.Point{}, draw.Src)
		}
	}
}

// drawTitle 在 (x, yCenter) 以白色 + 阴影绘制库名。
// 先单行二分找最大字号,放不下则多行 wrap 最多 3 行,末行超长加 "…"。
func drawTitle(dst *image.RGBA, name string, x, yCenter, maxW, maxH int) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}

	// 单行二分
	lowSize, highSize := 30.0, 140.0
	var bestSize float64
	var bestFace font.Face
	defer func() {
		if bestFace != nil {
			_ = bestFace.Close()
		}
	}()
	for lowSize <= highSize {
		mid := (lowSize + highSize) / 2
		face, err := makeFace(mid)
		if err != nil {
			return err
		}
		advance := font.MeasureString(face, name)
		if advance.Round() <= maxW {
			if bestFace != nil {
				_ = bestFace.Close()
			}
			bestFace = face
			bestSize = mid
			lowSize = mid + 2
		} else {
			_ = face.Close()
			highSize = mid - 2
		}
	}

	// 单行放得下 → 直接画
	if bestFace != nil {
		metrics := bestFace.Metrics()
		lineHeight := metrics.Ascent.Round() + metrics.Descent.Round()
		y := yCenter + metrics.Ascent.Round()/2
		_ = lineHeight
		_ = bestSize
		drawLineWithShadow(dst, name, x, y, bestFace)
		return nil
	}

	// 单行最小字号仍放不下 → 多行模式,字号用 48
	return drawWrappedTitle(dst, name, x, yCenter, maxW, maxH)
}

// drawWrappedTitle 以固定字号 48 多行 wrap,最多 3 行,超出末尾追加 "…"。
func drawWrappedTitle(dst *image.RGBA, name string, x, yCenter, maxW, _ int) error {
	const maxLines = 3
	const lineHeightFactor = 1.25
	size := 48.0
	for {
		if size < 24 {
			break
		}
		face, err := makeFace(size)
		if err != nil {
			return err
		}
		lines := wrapToLines(face, name, maxW, maxLines)
		metrics := face.Metrics()
		lineH := int(float64(metrics.Ascent.Round()+metrics.Descent.Round()) * lineHeightFactor)
		totalH := lineH * len(lines)
		// 如果最多 3 行仍有剩余字符,且已经最小字号,强制在末尾截断加 "…"
		if len(lines) <= maxLines || size <= 24 {
			yStart := yCenter - totalH/2 + metrics.Ascent.Round()
			for i, ln := range lines {
				drawLineWithShadow(dst, ln, x, yStart+i*lineH, face)
			}
			_ = face.Close()
			return nil
		}
		_ = face.Close()
		size -= 6
	}
	return nil
}

// wrapToLines 按"中文逐字、英文按空格"切分,使每行宽度 ≤ maxW。
// 最多产出 maxLines 行,超出部分末尾追加 "…"。
func wrapToLines(face font.Face, s string, maxW int, maxLines int) []string {
	tokens := splitTokens(s)
	var lines []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			lines = append(lines, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
	}
	for _, tok := range tokens {
		test := cur.String() + tok
		if font.MeasureString(face, test).Round() <= maxW {
			cur.WriteString(tok)
			continue
		}
		flush()
		cur.WriteString(tok)
		if len(lines) >= maxLines {
			break
		}
	}
	flush()

	if len(lines) > maxLines {
		// 末行加省略号
		lines = lines[:maxLines]
		lines[maxLines-1] = truncateWithEllipsis(face, lines[maxLines-1]+"…", maxW)
	}
	return lines
}

// splitTokens:中文/日文等 CJK 按单字切分,拉丁字符按空格切分成词 + 空格。
func splitTokens(s string) []string {
	var tokens []string
	var word strings.Builder
	flushWord := func() {
		if word.Len() > 0 {
			tokens = append(tokens, word.String())
			word.Reset()
		}
	}
	for _, r := range s {
		if isCJK(r) {
			flushWord()
			tokens = append(tokens, string(r))
		} else if r == ' ' {
			flushWord()
			tokens = append(tokens, " ")
		} else {
			word.WriteRune(r)
		}
	}
	flushWord()
	return tokens
}

func isCJK(r rune) bool {
	switch {
	case r >= 0x4E00 && r <= 0x9FFF: // CJK Unified Ideographs
		return true
	case r >= 0x3040 && r <= 0x30FF: // Hiragana + Katakana
		return true
	case r >= 0xAC00 && r <= 0xD7AF: // Hangul Syllables
		return true
	case r >= 0x3400 && r <= 0x4DBF: // CJK Extension A
		return true
	}
	return false
}

// truncateWithEllipsis 从末尾逐字符去掉,直到测得宽度 ≤ maxW 并在末尾保留 "…"。
func truncateWithEllipsis(face font.Face, s string, maxW int) string {
	if font.MeasureString(face, s).Round() <= maxW {
		return s
	}
	runes := []rune(s)
	for len(runes) > 1 {
		runes = runes[:len(runes)-2]
		trial := string(runes) + "…"
		if font.MeasureString(face, trial).Round() <= maxW {
			return trial
		}
	}
	return "…"
}

// drawLineWithShadow 先在 (x+3, y+3) 画半透明黑色阴影,再在 (x, y) 画白色主文字。
func drawLineWithShadow(dst *image.RGBA, text string, x, y int, face font.Face) {
	shadowColor := image.NewUniform(color.RGBA{0, 0, 0, 170})
	whiteColor := image.NewUniform(color.RGBA{255, 255, 255, 255})

	sd := &font.Drawer{
		Dst:  dst,
		Src:  shadowColor,
		Face: face,
		Dot:  fixed.P(x+3, y+3),
	}
	sd.DrawString(text)

	md := &font.Drawer{
		Dst:  dst,
		Src:  whiteColor,
		Face: face,
		Dot:  fixed.P(x, y),
	}
	md.DrawString(text)
}

// 编译时静态断言:ninegridStyle 实现 Generator。
var _ Generator = ninegridStyle{}

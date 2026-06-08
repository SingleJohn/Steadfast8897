package coverart

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"strconv"
	"strings"
	"sync"

	"github.com/disintegration/imaging"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// ninegridStyle 对标 jellyfin-library-poster:
//
//   - 左侧大面积淡色背景 + 主标题 + 橙色竖条 + 英文副标
//   - 右侧 3 列海报,每列 4 张(2:3 比例),整列统一倾斜 ~12°
//   - 每列 Y 起点错位,顶/底故意溢出画布形成"海报墙"视觉
//   - 海报有圆角 + 柔和投影
type ninegridStyle struct{}

func (ninegridStyle) Name() string            { return "ninegrid" }
func (ninegridStyle) Label() string           { return "九宫格拼贴" }
func (ninegridStyle) AspectRatio() (int, int) { return 16, 9 }

func init() {
	Register(ninegridStyle{})
}

// 输出尺寸(默认 1920x1080)。所有坐标常量都基于这个尺寸。
const (
	nineCanvasW = 1920
	nineCanvasH = 1080

	// 海报单张尺寸(2:3 比例)
	ninePosterW = 300
	ninePosterH = 450
	// 列内海报上下间距
	ninePosterGap = 22
	// 列水平间距(海报宽度 + 额外间距)
	nineColumnGap = 44
	// 每列海报数量
	ninePostersPerCol = 4
	// 列数
	nineColumns = 3
	// 整列旋转角度。imaging.Rotate 正数=逆时针;原版是顺时针倾斜(顶部偏右)→ 取负。
	nineRotateAngle = -12.0
	// 海报圆角半径
	nineCornerRadius = 14
)

func (ninegridStyle) Render(ctx context.Context, in Input) (Output, error) {
	w, h := in.OutputWidth, in.OutputHeight
	if w <= 0 {
		w = nineCanvasW
	}
	if h <= 0 {
		h = nineCanvasH
	}

	// 1. 加载 12 张海报(并发,失败用占位灰块),前 3 列×4 张
	need := ninePostersPerCol * nineColumns
	posters := loadPostersConcurrent(ctx, in.PosterPaths, need)

	// 2. 主色 → 背景色(柔和淡色)
	dominant := DominantColor(posters[0])
	bgTop := softBackground(dominant)
	bgBottom := softBackgroundDarker(dominant)

	base := image.NewRGBA(image.Rect(0, 0, w, h))
	drawVerticalGradient(base, bgTop, bgBottom)

	if err := ctx.Err(); err != nil {
		return Output{}, err
	}

	// 3. 绘制 3 列倾斜海报
	drawTiltedColumns(base, posters, w, h)

	if err := ctx.Err(); err != nil {
		return Output{}, err
	}

	// 4. 左侧标题 + 英文副标 + 橙色竖条
	if err := drawTitleBlock(base, in.LibraryName, in.CollectionType, w, h); err != nil {
		return Output{}, err
	}

	// 5. 右上角数量角标(若提供了 ItemCount)
	if in.ItemCount > 0 {
		if err := drawCountBadge(base, in.ItemCount, w); err != nil {
			return Output{}, err
		}
	}

	return Output{
		Image:   base,
		Mime:    "image/jpeg",
		Quality: 88,
	}, nil
}

// drawCountBadge 在画布右上角画一个橙色圆角矩形 + 白色数字(item 总数)。
func drawCountBadge(base *image.RGBA, count, canvasW int) error {
	face, err := makeFace(44)
	if err != nil {
		return err
	}
	defer face.Close()

	text := strconv.Itoa(count)
	advance := font.MeasureString(face, text).Round()
	metrics := face.Metrics()
	textH := metrics.Ascent.Round() + metrics.Descent.Round()

	padX, padY := 22, 10
	badgeW := advance + padX*2
	badgeH := textH + padY*2
	badgeX := canvasW - badgeW - 50
	badgeY := 50

	// 画一个带圆角的橙色矩形:先整体填色,再用透明像素裁四个角
	orange := color.RGBA{0xe4, 0x7e, 0x48, 0xff}
	radius := badgeH / 2
	if radius > 22 {
		radius = 22
	}
	drawRoundedRect(base, image.Rect(badgeX, badgeY, badgeX+badgeW, badgeY+badgeH), radius, orange)

	// 数字:白色,带轻微阴影
	textBaseY := badgeY + padY + metrics.Ascent.Round()
	drawStringWithShadow(base, text, badgeX+padX, textBaseY, face, color.RGBA{255, 255, 255, 255})
	return nil
}

// drawRoundedRect 在 dst 上画带圆角的实心矩形。
func drawRoundedRect(dst *image.RGBA, r image.Rectangle, radius int, c color.RGBA) {
	w, h := r.Dx(), r.Dy()
	if radius*2 > w {
		radius = w / 2
	}
	if radius*2 > h {
		radius = h / 2
	}
	rr := radius * radius
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// 四角外的像素不画
			var inCorner bool
			var cx, cy int
			switch {
			case x < radius && y < radius:
				inCorner, cx, cy = true, radius, radius
			case x >= w-radius && y < radius:
				inCorner, cx, cy = true, w-radius-1, radius
			case x < radius && y >= h-radius:
				inCorner, cx, cy = true, radius, h-radius-1
			case x >= w-radius && y >= h-radius:
				inCorner, cx, cy = true, w-radius-1, h-radius-1
			}
			if inCorner {
				dx, dy := x-cx, y-cy
				if dx*dx+dy*dy > rr {
					continue
				}
			}
			dst.SetRGBA(r.Min.X+x, r.Min.Y+y, c)
		}
	}
}


// loadPostersConcurrent 并发加载最多 n 张海报,路径不足时循环用已有的,单张失败返回占位。
func loadPostersConcurrent(ctx context.Context, paths []string, n int) []image.Image {
	out := make([]image.Image, n)
	var wg sync.WaitGroup
	pn := len(paths)
	for i := 0; i < n; i++ {
		idx := i
		var p string
		if pn > 0 {
			p = paths[idx%pn]
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ctx.Err() != nil || p == "" {
				out[idx] = placeholderTile(ninePosterW, ninePosterH)
				return
			}
			img, err := loadImage(p)
			if err != nil || img == nil {
				out[idx] = placeholderTile(ninePosterW, ninePosterH)
				return
			}
			out[idx] = img
		}()
	}
	wg.Wait()
	return out
}

// drawTiltedColumns 把 n 张海报按 3 列堆成整列后旋转,再贴到 base 右侧。
// 每列有独立的 Y 错位,产生视觉上的"每列高度不齐"。部分海报故意超出画布。
func drawTiltedColumns(base *image.RGBA, posters []image.Image, canvasW, canvasH int) {
	// 列画布尺寸:足够装下 4 张海报 + 间距 + 阴影 padding
	shadowPad := 24
	colW := ninePosterW + shadowPad*2
	colH := ninePostersPerCol*ninePosterH + (ninePostersPerCol-1)*ninePosterGap + shadowPad*2

	// 三列中心 X:整体靠右,但保证第 3 列旋转后不会完全溢出画布。
	// 左侧 [0, ~820] 留给标题文字区。
	colStep := ninePosterW + nineColumnGap
	baseColCenter := canvasW - colStep*2 - ninePosterW/2 - 60
	colCenterX := []int{
		baseColCenter,
		baseColCenter + colStep,
		baseColCenter + 2*colStep,
	}
	// Y 错位:让列与列之间不齐,形成错落视觉(单位:像素)
	colYJitter := []int{-90, 70, -30}

	for c := 0; c < nineColumns; c++ {
		colImg := image.NewNRGBA(image.Rect(0, 0, colW, colH))
		// 把 4 张海报竖向堆进列画布(含阴影)
		for r := 0; r < ninePostersPerCol; r++ {
			idx := c*ninePostersPerCol + r
			if idx >= len(posters) || posters[idx] == nil {
				continue
			}
			fitted := coverFit(posters[idx], ninePosterW, ninePosterH)
			rounded := roundCorners(fitted, nineCornerRadius)
			withShadow := withDropShadow(rounded, 0, 6, 8, 0.45)
			// withShadow 带 pad 的 bounds > rounded
			sb := withShadow.Bounds()
			dstX := (colW - sb.Dx()) / 2
			dstY := shadowPad + r*(ninePosterH+ninePosterGap) - (sb.Dy()-ninePosterH)/2
			draw.Draw(colImg,
				image.Rect(dstX, dstY, dstX+sb.Dx(), dstY+sb.Dy()),
				withShadow, sb.Min, draw.Over)
		}

		// 整列旋转(transparent 背景)
		rotated := imaging.Rotate(colImg, nineRotateAngle, color.Transparent)
		rb := rotated.Bounds()

		// 把旋转后的整列贴到 base:中心对齐 colCenterX[c],Y 基线为 canvas 垂直中心 + jitter
		cx := colCenterX[c]
		cy := canvasH/2 + colYJitter[c]
		dstX := cx - rb.Dx()/2
		dstY := cy - rb.Dy()/2
		draw.Draw(base,
			image.Rect(dstX, dstY, dstX+rb.Dx(), dstY+rb.Dy()),
			rotated, rb.Min, draw.Over)
	}
}

// drawTitleBlock 在 base 的左侧画:
//
//	主标题(库名,白色,大字号自适应)
//	+ 下方:橙色竖条 + 英文副标(TV / MOVIE / DOC / MEDIA)
func drawTitleBlock(base *image.RGBA, name, collectionType string, canvasW, canvasH int) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}

	// 左侧文字区域:x in [80, 780]
	const leftPad = 80
	titleMaxW := 700

	// 1. 主标题:二分找最大单行字号
	lowSize, highSize := 56.0, 170.0
	var bestFace font.Face
	for lowSize <= highSize {
		mid := (lowSize + highSize) / 2
		face, err := makeFace(mid)
		if err != nil {
			return err
		}
		advance := font.MeasureString(face, name)
		if advance.Round() <= titleMaxW {
			if bestFace != nil {
				_ = bestFace.Close()
			}
			bestFace = face
			lowSize = mid + 2
		} else {
			_ = face.Close()
			highSize = mid - 2
		}
	}

	// 副标题内容
	sub := subtitleForCollection(collectionType)

	// 2. 画主标题(如果单行放得下)
	var mainBottom int
	if bestFace != nil {
		defer bestFace.Close()
		metrics := bestFace.Metrics()
		ascent := metrics.Ascent.Round()
		descent := metrics.Descent.Round()
		// 主标题 baseline 位置:在画布纵向中心略偏上,给副标题留空间
		baseY := canvasH/2 - descent/2 + 10
		drawStringWithShadow(base, name, leftPad, baseY, bestFace, color.RGBA{255, 255, 255, 255})
		mainBottom = baseY + descent
		_ = ascent
	} else {
		// 单行最小字号也超宽 → 多行 wrap
		bottom, err := drawWrappedTitle(base, name, leftPad, canvasH/2, titleMaxW)
		if err != nil {
			return err
		}
		mainBottom = bottom
	}

	// 3. 副标题:橙色竖条 + 大写英文
	subSize := 30.0
	subFace, err := makeFace(subSize)
	if err != nil {
		return err
	}
	defer subFace.Close()
	subMetrics := subFace.Metrics()
	subH := subMetrics.Ascent.Round() + subMetrics.Descent.Round()
	subGapTop := 24
	subBaseY := mainBottom + subGapTop + subMetrics.Ascent.Round()

	// 橙色竖条:宽 5,高度与副标题基本一致
	barW := 5
	barH := subH - 2
	barX := leftPad
	barY := subBaseY - subMetrics.Ascent.Round() + 2
	drawFilledRect(base,
		image.Rect(barX, barY, barX+barW, barY+barH),
		color.RGBA{0xe4, 0x7e, 0x48, 0xff})

	// 副标题文字(带一点点字间距的大写英文)
	subX := leftPad + barW + 18
	drawStringWithShadow(base, sub, subX, subBaseY, subFace, color.RGBA{255, 255, 255, 220})

	_ = canvasW
	return nil
}

// subtitleForCollection 把库类型映射到英文副标。
func subtitleForCollection(ct string) string {
	switch strings.ToLower(strings.TrimSpace(ct)) {
	case "movies":
		return "M O V I E"
	case "tvshows":
		return "T V"
	case "mixed":
		return "M I X E D"
	case "music":
		return "M U S I C"
	case "boxsets":
		return "C O L L E C T I O N"
	case "homevideos":
		return "H O M E"
	default:
		return "M E D I A"
	}
}

// drawWrappedTitle 用固定字号 72 多行 wrap,最多 3 行,末行超长追加 "…"。
// 返回最后一行的 baseline + descent(供副标题定位)。
func drawWrappedTitle(dst *image.RGBA, name string, x, yCenter, maxW int) (int, error) {
	const maxLines = 3
	const lineHeightFactor = 1.18
	size := 72.0
	for size >= 36 {
		face, err := makeFace(size)
		if err != nil {
			return 0, err
		}
		lines := wrapToLines(face, name, maxW, maxLines)
		metrics := face.Metrics()
		lineH := int(float64(metrics.Ascent.Round()+metrics.Descent.Round()) * lineHeightFactor)
		totalH := lineH * len(lines)
		if len(lines) <= maxLines || size <= 36 {
			yStart := yCenter - totalH/2 + metrics.Ascent.Round()
			for i, ln := range lines {
				drawStringWithShadow(dst, ln, x, yStart+i*lineH, face, color.RGBA{255, 255, 255, 255})
			}
			bottom := yStart + (len(lines)-1)*lineH + metrics.Descent.Round()
			_ = face.Close()
			return bottom, nil
		}
		_ = face.Close()
		size -= 6
	}
	return yCenter, nil
}

// wrapToLines 按"中文逐字、英文按空格"切分。
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
		lines = lines[:maxLines]
		lines[maxLines-1] = truncateWithEllipsis(face, lines[maxLines-1]+"…", maxW)
	}
	return lines
}

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
	case r >= 0x4E00 && r <= 0x9FFF:
		return true
	case r >= 0x3040 && r <= 0x30FF:
		return true
	case r >= 0xAC00 && r <= 0xD7AF:
		return true
	case r >= 0x3400 && r <= 0x4DBF:
		return true
	}
	return false
}

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

// drawStringWithShadow 先画半透明黑色阴影,再画主文字。
func drawStringWithShadow(dst *image.RGBA, text string, x, y int, face font.Face, fg color.RGBA) {
	shadow := image.NewUniform(color.RGBA{0, 0, 0, 150})
	fill := image.NewUniform(fg)

	sd := &font.Drawer{
		Dst:  dst,
		Src:  shadow,
		Face: face,
		Dot:  fixed.P(x+2, y+3),
	}
	sd.DrawString(text)

	md := &font.Drawer{
		Dst:  dst,
		Src:  fill,
		Face: face,
		Dot:  fixed.P(x, y),
	}
	md.DrawString(text)
}

// 编译时静态断言:ninegridStyle 实现 Generator。
var _ Generator = ninegridStyle{}

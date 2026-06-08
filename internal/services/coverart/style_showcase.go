package coverart

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strconv"
	"strings"

	"github.com/disintegration/imaging"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

type showcaseStyle struct{}

func (showcaseStyle) Name() string            { return "showcase" }
func (showcaseStyle) Label() string           { return "横幅陈列" }
func (showcaseStyle) AspectRatio() (int, int) { return 16, 9 }

func init() {
	Register(showcaseStyle{})
}

const (
	showcaseCanvasW = 1920
	showcaseCanvasH = 1080

	showcasePosterCount = 5
	showcasePosterW     = 250
	showcasePosterH     = 450
	showcasePosterGap   = 30
	showcasePosterX     = 520
	showcasePosterY     = 500
	showcaseCorner      = 9
	showcaseTextX       = 120
)

func (showcaseStyle) Render(ctx context.Context, in Input) (Output, error) {
	w, h := in.OutputWidth, in.OutputHeight
	if w <= 0 {
		w = showcaseCanvasW
	}
	if h <= 0 {
		h = showcaseCanvasH
	}

	materials := normalizeShowcaseMaterials(in)
	posters := loadShowcasePosters(ctx, materials, showcasePosterCount)
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}

	base := image.NewRGBA(image.Rect(0, 0, w, h))
	drawShowcaseBackground(base, materials, posters)
	drawShowcaseOverlays(base)

	icon := optionString(in.Options, "Icon", defaultShowcaseIcon(in.CollectionType, in.LibraryName))
	showPosterTitles := optionBool(in.Options, "ShowPosterTitles", true)
	showCount := optionBool(in.Options, "ShowCount", true)

	drawShowcaseIcon(base, icon, showcaseTextX, 261, 130)
	if err := drawShowcaseText(base, in.LibraryName, in.CollectionType, in.ItemCount, showCount); err != nil {
		return Output{}, err
	}
	if err := drawShowcasePosters(base, materials, posters, showPosterTitles); err != nil {
		return Output{}, err
	}

	return Output{
		Image:   base,
		Mime:    "image/jpeg",
		Quality: 90,
	}, nil
}

func normalizeShowcaseMaterials(in Input) []Material {
	if len(in.Materials) > 0 {
		return in.Materials
	}
	out := make([]Material, 0, len(in.PosterPaths))
	for _, p := range in.PosterPaths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		out = append(out, Material{PosterPath: p})
	}
	return out
}

func loadShowcasePosters(ctx context.Context, materials []Material, n int) []image.Image {
	out := make([]image.Image, n)
	for i := 0; i < n; i++ {
		if ctx.Err() != nil {
			break
		}
		if len(materials) == 0 || strings.TrimSpace(materials[i%len(materials)].PosterPath) == "" {
			out[i] = placeholderTile(showcasePosterW, showcasePosterH)
			continue
		}
		img, err := loadImage(materials[i%len(materials)].PosterPath)
		if err != nil || img == nil {
			out[i] = placeholderTile(showcasePosterW, showcasePosterH)
			continue
		}
		out[i] = img
	}
	return out
}

func drawShowcaseBackground(base *image.RGBA, materials []Material, posters []image.Image) {
	if bg := loadBestBackdrop(materials); bg != nil {
		fitted := coverFit(bg, base.Bounds().Dx(), base.Bounds().Dy())
		soft := imaging.Blur(fitted, 1.15)
		draw.Draw(base, base.Bounds(), soft, soft.Bounds().Min, draw.Src)
		return
	}
	dominant := fallbackDominant
	if len(posters) > 0 && posters[0] != nil {
		dominant = DominantColor(posters[0])
	}
	drawShowcaseGradient(base, dominant)
}

func loadBestBackdrop(materials []Material) image.Image {
	for _, m := range materials {
		p := strings.TrimSpace(m.BackdropPath)
		if p == "" {
			continue
		}
		img, err := loadImage(p)
		if err == nil && img != nil {
			return img
		}
	}
	return nil
}

func drawShowcaseGradient(dst *image.RGBA, seed color.RGBA) {
	b := dst.Bounds()
	hue, sat, _ := rgbToHSL(seed.R, seed.G, seed.B)
	if sat < 0.18 {
		hue = 260
	}
	left := hslToRGB(hue+18, 0.46, 0.24)
	right := hslToRGB(hue-22, 0.58, 0.12)
	top := hslToRGB(hue+42, 0.40, 0.30)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		ty := float64(y-b.Min.Y) / float64(b.Dy()-1)
		for x := b.Min.X; x < b.Max.X; x++ {
			tx := float64(x-b.Min.X) / float64(b.Dx()-1)
			c := lerpColor(left, right, tx)
			c = lerpColor(c, top, math.Max(0, 1.0-tx*1.45-ty*0.62)*0.45)
			halo := math.Exp(-(math.Pow((tx-0.62)/0.26, 2) + math.Pow((ty-0.24)/0.32, 2)))
			c = lerpColor(c, hslToRGB(hue+75, 0.55, 0.45), halo*0.30)
			dst.SetRGBA(x, y, c)
		}
	}
}

func drawShowcaseOverlays(dst *image.RGBA) {
	b := dst.Bounds()
	w, h := b.Dx(), b.Dy()
	for y := 0; y < h; y++ {
		ty := float64(y) / float64(h-1)
		for x := 0; x < w; x++ {
			tx := float64(x) / float64(w-1)
			edge := math.Max(math.Abs(tx-0.52)/0.52, math.Abs(ty-0.50)/0.50)
			vignette := clampFloat((edge-0.35)/0.65, 0, 1) * 0.58
			leftShade := math.Max(0, 1.0-tx*1.85) * 0.34
			bottomShade := math.Pow(ty, 2.4) * 0.26
			topShade := math.Pow(1.0-ty, 3.0) * 0.16
			alpha := clampFloat(0.30+vignette+leftShade+bottomShade+topShade, 0, 0.82)
			blendPixelRGBA(dst, x, y, color.RGBA{0, 0, 0, uint8(alpha * 255)})
		}
	}
}

func drawShowcaseText(base *image.RGBA, name, collectionType string, count int, showCount bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	titleFace, err := bestFaceForWidth(name, 94, 126, 360)
	if err != nil {
		return err
	}
	defer titleFace.Close()

	metrics := titleFace.Metrics()
	titleY := 540
	drawShowcaseString(base, name, showcaseTextX, titleY, titleFace)

	subFace, err := makeFace(40)
	if err != nil {
		return err
	}
	defer subFace.Close()
	sub := showcaseSubtitle(collectionType, name)
	subX := showcaseTextX + 4
	drawShowcaseString(base, sub, subX, titleY+metrics.Descent.Round()+62, subFace)

	lineY := titleY + metrics.Descent.Round() + 94
	drawFilledRect(base, image.Rect(subX, lineY, subX+74, lineY+4), color.RGBA{255, 255, 255, 210})

	if showCount && count > 0 {
		countFace, err := makeFace(44)
		if err != nil {
			return err
		}
		defer countFace.Close()
		text := "共 " + strconv.Itoa(count) + " 部"
		if strings.EqualFold(collectionType, "music") {
			text = "共 " + strconv.Itoa(count) + " 张专辑"
		}
		drawShowcaseString(base, text, subX, lineY+68, countFace)
	}
	return nil
}

func drawShowcaseString(dst *image.RGBA, text string, x, y int, face font.Face) {
	shadow := image.NewUniform(color.RGBA{0, 0, 0, 92})
	fill := image.NewUniform(color.RGBA{255, 255, 255, 255})
	sd := &font.Drawer{
		Dst:  dst,
		Src:  shadow,
		Face: face,
		Dot:  fixed.P(x+1, y+2),
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

func bestFaceForWidth(text string, minSize, maxSize, maxW float64) (font.Face, error) {
	var best font.Face
	for size := maxSize; size >= minSize; size -= 4 {
		face, err := makeFace(size)
		if err != nil {
			return nil, err
		}
		if font.MeasureString(face, text).Round() <= int(maxW) {
			if best != nil {
				_ = best.Close()
			}
			best = face
			break
		}
		_ = face.Close()
	}
	if best != nil {
		return best, nil
	}
	return makeFace(minSize)
}

func drawShowcasePosters(base *image.RGBA, materials []Material, posters []image.Image, showTitles bool) error {
	titleFace, err := makeFace(27)
	if err != nil {
		return err
	}
	defer titleFace.Close()

	for i := 0; i < showcasePosterCount; i++ {
		if i >= len(posters) || posters[i] == nil {
			continue
		}
		x := showcasePosterX + i*(showcasePosterW+showcasePosterGap)
		y := showcasePosterY
		fitted := coverFit(posters[i], showcasePosterW, showcasePosterH)
		rounded := roundCorners(fitted, showcaseCorner)
		card := withDropShadow(rounded, 0, 8, 10, 0.55)
		cb := card.Bounds()
		cardX := x - (cb.Dx()-showcasePosterW)/2
		cardY := y - (cb.Dy()-showcasePosterH)/2
		draw.Draw(base, image.Rect(cardX, cardY, cardX+cb.Dx(), cardY+cb.Dy()), card, cb.Min, draw.Over)

		border := color.RGBA{255, 255, 255, 86}
		drawStrokeRoundedRect(base, image.Rect(x, y, x+showcasePosterW, y+showcasePosterH), showcaseCorner, 2, border)
		if !showTitles {
			continue
		}
		barH := 82
		bar := image.Rect(x+2, y+showcasePosterH-barH, x+showcasePosterW-2, y+showcasePosterH-2)
		drawRoundedBottomOverlay(base, bar, showcaseCorner-2, color.RGBA{0, 0, 0, 150})
		title := ""
		if len(materials) > 0 {
			title = materials[i%len(materials)].Title
		}
		if title == "" {
			title = " "
		}
		title = truncateWithEllipsis(titleFace, title, showcasePosterW-34)
		tw := font.MeasureString(titleFace, title).Round()
		tm := titleFace.Metrics()
		tx := x + (showcasePosterW-tw)/2
		ty := y + showcasePosterH - barH/2 + (tm.Ascent.Round()-tm.Descent.Round())/2
		drawPlainString(base, title, tx, ty, titleFace, color.RGBA{218, 222, 228, 235})
	}
	return nil
}

func drawPlainString(dst *image.RGBA, text string, x, y int, face font.Face, fg color.RGBA) {
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(fg),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(text)
}

func showcaseSubtitle(collectionType, name string) string {
	switch strings.ToLower(strings.TrimSpace(collectionType)) {
	case "movies":
		return "MOVIES"
	case "tvshows":
		return "TV SHOWS"
	case "mixed":
		return "MOVIES & TV"
	case "music":
		return "MUSIC"
	case "boxsets":
		return "COLLECTIONS"
	case "homevideos":
		return "VIDEOS"
	default:
		lowerName := strings.ToLower(name)
		switch {
		case strings.Contains(lowerName, "动漫") || strings.Contains(lowerName, "动画") || strings.Contains(lowerName, "anime"):
			return "ANIME"
		case strings.Contains(lowerName, "纪录") || strings.Contains(lowerName, "document"):
			return "DOCUMENTARIES"
		case strings.Contains(lowerName, "少儿") || strings.Contains(lowerName, "kids"):
			return "KIDS"
		default:
			return "MEDIA"
		}
	}
}

func defaultShowcaseIcon(collectionType, name string) string {
	lowerName := strings.ToLower(name)
	switch {
	case strings.Contains(lowerName, "动漫") || strings.Contains(lowerName, "动画") || strings.Contains(lowerName, "anime"):
		return "anime"
	case strings.Contains(lowerName, "纪录") || strings.Contains(lowerName, "document"):
		return "documentary"
	case strings.Contains(lowerName, "少儿") || strings.Contains(lowerName, "kids"):
		return "kids"
	}
	switch strings.ToLower(strings.TrimSpace(collectionType)) {
	case "movies":
		return "movie"
	case "tvshows":
		return "tv"
	case "mixed":
		return "media"
	case "music":
		return "music"
	default:
		return "media"
	}
}

func optionString(options map[string]any, key, fallback string) string {
	if options == nil {
		return fallback
	}
	if v, ok := options[key].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func optionBool(options map[string]any, key string, fallback bool) bool {
	if options == nil {
		return fallback
	}
	switch v := options[key].(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		}
	}
	return fallback
}

func drawShowcaseIcon(dst *image.RGBA, icon string, x, y, size int) {
	c := color.RGBA{255, 255, 255, 245}
	switch strings.ToLower(strings.TrimSpace(icon)) {
	case "tv":
		drawTVIcon(dst, x, y, size, c)
	case "music":
		drawMusicIcon(dst, x, y, size, c)
	case "anime":
		drawAnimeIcon(dst, x, y, size, c)
	case "documentary":
		drawCameraIcon(dst, x, y, size, c)
	case "kids":
		drawKidsIcon(dst, x, y, size, c)
	case "media":
		drawMediaIcon(dst, x, y, size, c)
	default:
		drawMovieIcon(dst, x, y, size, c)
	}
}

func drawMovieIcon(dst *image.RGBA, x, y, size int, c color.RGBA) {
	th := maxInt(6, size/18)
	cx := x + size/2
	cy := y + size/2
	r := size / 3
	drawCircleStroke(dst, cx, cy, r, th, c)
	for i := 0; i < 6; i++ {
		ang := float64(i) * math.Pi / 3
		drawCircleFilled(dst, cx+int(math.Cos(ang)*float64(r/2)), cy+int(math.Sin(ang)*float64(r/2)), size/18, c)
	}
	drawThickLine(dst, x+size/4, y+size*3/4, x+size, y+size*2/3, th, c)
}

func drawTVIcon(dst *image.RGBA, x, y, size int, c color.RGBA) {
	th := maxInt(6, size/18)
	rect := image.Rect(x+8, y+34, x+size-8, y+size-22)
	drawStrokeRoundedRect(dst, rect, size/12, th, c)
	drawThickLine(dst, x+size/2, y+34, x+size/3, y+8, th, c)
	drawThickLine(dst, x+size/2, y+34, x+size*2/3, y+8, th, c)
	drawThickLine(dst, x+size/3, y+size-12, x+size*2/3, y+size-12, th, c)
}

func drawMusicIcon(dst *image.RGBA, x, y, size int, c color.RGBA) {
	th := maxInt(6, size/18)
	drawCircleStroke(dst, x+size/3, y+size*2/3, size/8, th, c)
	drawCircleStroke(dst, x+size*2/3, y+size/2, size/8, th, c)
	drawThickLine(dst, x+size/3+size/8, y+size*2/3, x+size/3+size/8, y+size/4, th, c)
	drawThickLine(dst, x+size*2/3+size/8, y+size/2, x+size*2/3+size/8, y+size/7, th, c)
	drawThickLine(dst, x+size/3+size/8, y+size/4, x+size*2/3+size/8, y+size/7, th, c)
}

func drawAnimeIcon(dst *image.RGBA, x, y, size int, c color.RGBA) {
	drawCircleFilled(dst, x+size/2, y+size/3, size/4, c)
	drawFilledRect(dst, image.Rect(x+size/3, y+size/3, x+size*2/3, y+size*3/4), c)
	drawCircleFilled(dst, x+size/4, y+size*2/5, size/8, c)
	drawCircleFilled(dst, x+size*3/4, y+size*2/5, size/8, c)
	drawFilledRect(dst, image.Rect(x+size/5, y+size/5, x+size*4/5, y+size/3), c)
}

func drawCameraIcon(dst *image.RGBA, x, y, size int, c color.RGBA) {
	th := maxInt(6, size/18)
	body := image.Rect(x+14, y+size/3, x+size-14, y+size*3/4)
	drawStrokeRoundedRect(dst, body, size/14, th, c)
	drawCircleStroke(dst, x+size/2, y+size*13/24, size/8, th, c)
	drawThickLine(dst, x+size-14, y+size/2, x+size, y+size*2/5, th, c)
	drawThickLine(dst, x+size-14, y+size/2, x+size, y+size*3/5, th, c)
	drawFilledRect(dst, image.Rect(x+size/4, y+size/4, x+size/2, y+size/3), c)
}

func drawKidsIcon(dst *image.RGBA, x, y, size int, c color.RGBA) {
	th := maxInt(6, size/18)
	cx, cy := x+size/2, y+size/2
	drawCircleStroke(dst, cx, cy, size/3, th, c)
	drawCircleFilled(dst, cx-size/9, cy-size/12, size/24, c)
	drawCircleFilled(dst, cx+size/9, cy-size/12, size/24, c)
	drawArcStroke(dst, cx, cy+size/20, size/7, 0.18*math.Pi, 0.82*math.Pi, th, c)
}

func drawMediaIcon(dst *image.RGBA, x, y, size int, c color.RGBA) {
	th := maxInt(6, size/18)
	drawStrokeRoundedRect(dst, image.Rect(x+18, y+30, x+size-28, y+size-28), size/15, th, c)
	drawStrokeRoundedRect(dst, image.Rect(x+32, y+18, x+size-14, y+size-42), size/15, th, color.RGBA{255, 255, 255, 170})
	drawThickLine(dst, x+size/2-12, y+size/2-18, x+size/2-12, y+size/2+26, th, c)
	drawThickLine(dst, x+size/2-12, y+size/2+26, x+size/2+26, y+size/2+4, th, c)
}

func drawRoundedBottomOverlay(dst *image.RGBA, r image.Rectangle, radius int, c color.RGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			localX := x - r.Min.X
			localY := y - r.Min.Y
			w, h := r.Dx(), r.Dy()
			if localY >= h-radius {
				if localX < radius {
					dx, dy := localX-radius, localY-(h-radius)
					if dx*dx+dy*dy > radius*radius {
						continue
					}
				}
				if localX >= w-radius {
					dx, dy := localX-(w-radius-1), localY-(h-radius)
					if dx*dx+dy*dy > radius*radius {
						continue
					}
				}
			}
			blendPixelRGBA(dst, x, y, c)
		}
	}
}

func drawStrokeRoundedRect(dst *image.RGBA, r image.Rectangle, radius, width int, c color.RGBA) {
	if width <= 0 {
		return
	}
	inner := r.Inset(width)
	innerRadius := radius - width
	if innerRadius < 0 {
		innerRadius = 0
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if !insideRoundedRect(x, y, r, radius) {
				continue
			}
			if image.Pt(x, y).In(inner) && insideRoundedRect(x, y, inner, innerRadius) {
				continue
			}
			blendPixelRGBA(dst, x, y, c)
		}
	}
}

func insideRoundedRect(x, y int, r image.Rectangle, radius int) bool {
	if !image.Pt(x, y).In(r) {
		return false
	}
	if radius <= 0 {
		return true
	}
	if radius*2 > r.Dx() {
		radius = r.Dx() / 2
	}
	if radius*2 > r.Dy() {
		radius = r.Dy() / 2
	}
	cx, cy := x, y
	if x < r.Min.X+radius {
		cx = r.Min.X + radius
	} else if x >= r.Max.X-radius {
		cx = r.Max.X - radius - 1
	}
	if y < r.Min.Y+radius {
		cy = r.Min.Y + radius
	} else if y >= r.Max.Y-radius {
		cy = r.Max.Y - radius - 1
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= radius*radius
}

func drawThickLine(dst *image.RGBA, x0, y0, x1, y1, width int, c color.RGBA) {
	dx := float64(x1 - x0)
	dy := float64(y1 - y0)
	steps := int(math.Max(math.Abs(dx), math.Abs(dy)))
	if steps == 0 {
		drawCircleFilled(dst, x0, y0, width/2, c)
		return
	}
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x := int(math.Round(float64(x0) + dx*t))
		y := int(math.Round(float64(y0) + dy*t))
		drawCircleFilled(dst, x, y, width/2, c)
	}
}

func drawCircleFilled(dst *image.RGBA, cx, cy, r int, c color.RGBA) {
	rr := r * r
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= rr {
				blendPixelRGBA(dst, x, y, c)
			}
		}
	}
}

func drawCircleStroke(dst *image.RGBA, cx, cy, r, width int, c color.RGBA) {
	outer := r * r
	inner := (r - width) * (r - width)
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := x-cx, y-cy
			d2 := dx*dx + dy*dy
			if d2 <= outer && d2 >= inner {
				blendPixelRGBA(dst, x, y, c)
			}
		}
	}
}

func drawArcStroke(dst *image.RGBA, cx, cy, r int, start, end float64, width int, c color.RGBA) {
	steps := maxInt(24, int(float64(r)*math.Abs(end-start)))
	for i := 0; i <= steps; i++ {
		t := start + (end-start)*float64(i)/float64(steps)
		x := cx + int(math.Round(math.Cos(t)*float64(r)))
		y := cy + int(math.Round(math.Sin(t)*float64(r)))
		drawCircleFilled(dst, x, y, maxInt(1, width/2), c)
	}
}

func blendPixelRGBA(dst *image.RGBA, x, y int, src color.RGBA) {
	if !image.Pt(x, y).In(dst.Bounds()) || src.A == 0 {
		return
	}
	i := dst.PixOffset(x, y)
	a := float64(src.A) / 255.0
	dst.Pix[i+0] = uint8(float64(src.R)*a + float64(dst.Pix[i+0])*(1-a))
	dst.Pix[i+1] = uint8(float64(src.G)*a + float64(dst.Pix[i+1])*(1-a))
	dst.Pix[i+2] = uint8(float64(src.B)*a + float64(dst.Pix[i+2])*(1-a))
	dst.Pix[i+3] = 255
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var _ Generator = showcaseStyle{}

// Package coverart 提供媒体库封面图的自动生成能力。
// Generator 接口允许未来扩展多种风格(首期仅 ninegrid);
// 上层 handler 通过 registry.Get(name) 拿到风格实现后调用 Render。
package coverart

import (
	"context"
	"errors"
	"image"

	"github.com/google/uuid"
)

// ErrNoPosters 表示媒体库下没有可用于生成封面的海报素材。
var ErrNoPosters = errors.New("coverart: no poster material available in library")

// ErrStyleNotFound 表示请求的风格未注册。
var ErrStyleNotFound = errors.New("coverart: style not found")

// ErrFontMissing 表示字体文件缺失,无法渲染文字。
var ErrFontMissing = errors.New("coverart: font asset missing (put NotoSansSC-Bold.otf under internal/services/coverart/assets/fonts/ or data/fonts/)")

// ErrBusy 表示同一媒体库已有正在进行中的生成任务。
var ErrBusy = errors.New("coverart: generation already in progress for this library")

// Input 描述一次封面渲染所需的全部输入。
// PosterPaths 由 fetch.go 填充,保证长度为 9(不足时循环补齐)。
type Input struct {
	LibraryID      uuid.UUID
	LibraryName    string
	CollectionType string // movies / tvshows / ...
	PosterPaths    []string
	Options        map[string]any // 风格特化参数,预留给未来风格
	OutputWidth    int            // 默认 1920
	OutputHeight   int            // 默认 1080
}

// Output 封装渲染结果;Mime/Quality 用于 handler 写回磁盘时复用。
type Output struct {
	Image   image.Image
	Mime    string // "image/jpeg"
	Quality int    // 88
}

// Generator 是所有封面生成风格必须实现的接口。
type Generator interface {
	Name() string            // 机器名,如 "ninegrid"
	Label() string           // 面向用户的中文展示名
	AspectRatio() (w, h int) // 建议的宽高比,前端展示/校验用
	Render(ctx context.Context, in Input) (Output, error)
}

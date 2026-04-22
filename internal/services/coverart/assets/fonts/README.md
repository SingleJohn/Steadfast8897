# 封面标题字体

构建前必须确保本目录下有一个可用的中文字体,否则封面生成功能会在运行时返回 424(字体缺失),但**不会影响编译**(`go:embed` 能匹配到 README.md,不会阻塞 build)。

## 推荐:跑一次下载脚本

```bash
bash scripts/fetch-fonts.sh
```

幂等:文件已存在且尺寸合理时直接跳过。
支持 Linux / macOS / Windows(Git Bash)。
多镜像 fallback(GitHub raw → jsDelivr → raw.githubusercontent),某一源挂了会自动切换。

CI/CD(GitHub Actions、Dockerfile)已在 `go build` 之前自动调用本脚本,无需手工干预。

## 手动下载

下载以下任一文件放到本目录:

- `NotoSansSC-Bold.otf`(推荐,脚本默认下载的就是这个文件)
- 或 `NotoSansSC-Bold.ttf`

直链:
<https://github.com/notofonts/noto-cjk/raw/main/Sans/OTF/SimplifiedChinese/NotoSansCJKsc-Bold.otf>

## 运行时 fallback 顺序

1. 本目录(`internal/services/coverart/assets/fonts/*.otf|*.ttf`,通过 `go:embed` 打入二进制)
2. 外部目录(`data/fonts/*.otf|*.ttf`,部署后再换字体用)
3. 都找不到 → 封面生成 API 返回 `font asset missing`

## 许可证

Noto 字体由 Google 以 SIL Open Font License 1.1 发布,允许商用、再分发、嵌入到二进制。

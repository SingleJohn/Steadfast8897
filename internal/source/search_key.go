package source

import (
	"fmt"
	"strings"

	"fyms/internal/repository"
)

func SourceItemSearchKey(item repository.SourceItem) string {
	title := NormalizeSourceSearchTitle(item.Title)
	if title == "" {
		return fmt.Sprintf("provider:%d:%s", item.ProviderID, item.SourceItemID)
	}
	if item.Year != nil && *item.Year > 0 {
		return fmt.Sprintf("%s:%d", title, *item.Year)
	}
	return title
}

func NormalizeSourceSearchTitle(title string) string {
	title = strings.ToLower(cleanCMSValue(title))
	// 繁→简折叠:让简繁标题/关键词归一到同一规范键,跨脚本去重与匹配不再分裂。
	title = foldTraditionalToSimplified(title)
	replacer := strings.NewReplacer(" ", "", "　", "", "-", "", "_", "", ":", "", "：", "", "·", "")
	return replacer.Replace(title)
}

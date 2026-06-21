package source

import "strings"

func NormalizeCMSKind(typeName string) string {
	typeName = cleanCMSValue(typeName)
	lower := strings.ToLower(typeName)
	switch {
	case containsAny(typeName, "动漫", "动画", "番剧") || strings.Contains(lower, "anime"):
		return "anime"
	case containsAny(typeName, "综艺", "真人秀", "脱口秀", "晚会"):
		return "variety"
	case containsAny(typeName, "纪录", "记录", "纪实") || strings.Contains(lower, "documentary"):
		return "documentary"
	case containsAny(typeName, "剧", "连续", "电视剧", "欧美剧", "日韩剧", "国产剧", "港台剧"):
		return "series"
	case containsAny(typeName, "电影", "片", "影院", "动作", "喜剧", "爱情", "科幻", "恐怖", "剧情", "战争", "悬疑"):
		return "movie"
	default:
		return "unknown"
	}
}

func NormalizeCMSRegion(area string) *string {
	area = cleanCMSValue(area)
	if area == "" {
		return nil
	}
	switch {
	case containsAny(area, "大陆", "内地", "中国", "国产", "华语"):
		return ptrString("CN")
	case containsAny(area, "香港", "港"):
		return ptrString("HK")
	case containsAny(area, "台湾", "台"):
		return ptrString("TW")
	case containsAny(area, "美国", "欧美", "英国", "法国", "德国", "意大利", "西班牙", "欧洲"):
		if containsAny(area, "美国") {
			return ptrString("US")
		}
		return ptrString("EU")
	case containsAny(area, "日本", "日韩"):
		return ptrString("JP")
	case containsAny(area, "韩国"):
		return ptrString("KR")
	case containsAny(area, "泰国", "印度", "海外", "其他", "国外"):
		return ptrString("Foreign")
	default:
		return ptrString("Foreign")
	}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func ptrString(value string) *string {
	return &value
}

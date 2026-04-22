package coverart

import "sync"

var (
	registryMu sync.RWMutex
	registry   = map[string]Generator{}
	order      []string // 保留注册顺序,List() 按此顺序返回
)

// Register 向全局 registry 注册一个风格实现。
// 同名风格会覆盖前者,主要为测试/开发期便利。
func Register(g Generator) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, ok := registry[g.Name()]; !ok {
		order = append(order, g.Name())
	}
	registry[g.Name()] = g
}

// Get 按 name 查找已注册风格。
func Get(name string) (Generator, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	g, ok := registry[name]
	return g, ok
}

// List 按注册顺序返回所有可用风格。
func List() []Generator {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Generator, 0, len(order))
	for _, n := range order {
		if g, ok := registry[n]; ok {
			out = append(out, g)
		}
	}
	return out
}

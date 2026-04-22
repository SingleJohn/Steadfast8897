package coverart

import (
	"sync"

	"github.com/google/uuid"
)

// busyMu 保护 busySet。
// 同一库的两次生成请求在第一次未完成前会拿到 ErrBusy。
var (
	busyMu  sync.Mutex
	busySet = map[uuid.UUID]struct{}{}
)

// AcquireBusy 尝试占用该库的生成槽位。返回 release 函数;
// 已被占用时返回 nil + ErrBusy。
func AcquireBusy(id uuid.UUID) (release func(), err error) {
	busyMu.Lock()
	defer busyMu.Unlock()
	if _, ok := busySet[id]; ok {
		return nil, ErrBusy
	}
	busySet[id] = struct{}{}
	return func() {
		busyMu.Lock()
		delete(busySet, id)
		busyMu.Unlock()
	}, nil
}

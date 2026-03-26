package store

import (
	"strconv"
	"sync/atomic"
	"time"
)

type Revision struct {
	v atomic.Uint64
}

func NewRevision() *Revision {
	r := &Revision{}
	r.v.Store(uint64(time.Now().UnixNano()))
	return r
}

func (r *Revision) Bump() {
	r.v.Add(1)
}

func (r *Revision) ETag() string {
	return `"` + strconv.FormatUint(r.v.Load(), 36) + `"`
}

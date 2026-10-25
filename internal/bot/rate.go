package bot

import (
	"context"
	"sync"
	"time"
)

type rateLimiter struct {
	mu         sync.Mutex
	global     time.Time
	chats      map[int64]time.Time
	operations uint64
}

func newRateLimiter() *rateLimiter { return &rateLimiter{chats: make(map[int64]time.Time)} }
func (r *rateLimiter) Wait(ctx context.Context, chat int64, group bool) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r.mu.Lock()
		now := time.Now()
		at := now
		if r.global.After(at) {
			at = r.global
		}
		if r.chats[chat].After(at) {
			at = r.chats[chat]
		}
		if at.After(now) {
			r.mu.Unlock()
			if !pause(ctx, time.Until(at)) {
				return ctx.Err()
			}
			continue
		}
		r.global = now.Add(time.Second / 20)
		gap := time.Second
		if group {
			gap = 3100 * time.Millisecond
		}
		r.chats[chat] = now.Add(gap)
		r.operations++
		if r.operations%1000 == 0 {
			for id, t := range r.chats {
				if t.Before(now) {
					delete(r.chats, id)
				}
			}
		}
		r.mu.Unlock()
		return nil
	}
}

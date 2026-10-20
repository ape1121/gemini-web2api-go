package app

// Video quota routing. An account whose video budget is used up is skipped for new
// videos until it resets, and a job that lands on such an account is moved to the next
// account inside the same job (a quota refusal consumes nothing). When every enabled
// account is in its cooldown, POST /v1/videos answers 429 video_quota_exhausted with
// Retry-After, and a running job fails with the same code and the earliest reset, so
// the client can wait instead of resubmitting into a pool that cannot make videos.
//
// The cooldown starts at videoCooldownDefault and is then corrected in the background
// to the account's real reset time, read from its Gemini history (videoQuotaFor:
// Google's own timestamp on the refusal, or first video in the window + 5 h). It lives
// in memory: after a restart each exhausted account costs one fast refusal to relearn.

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

const (
	videoCooldownDefault = time.Hour
	videoCooldownMax     = quotaWindow + time.Hour
)

var (
	videoCooldownMu sync.Mutex
	videoCooldown   = map[int64]int64{} // account id → unix time its video budget resets
	// Learns the real reset time of an exhausted account; replaced in tests.
	videoResetLookup = func(id int64) int64 {
		a := accountByID(id)
		if a == nil {
			return 0
		}
		return videoQuotaFor(*a, time.Now(), 8*time.Hour).ResetAt
	}
)

// markVideoExhausted puts an account in its cooldown and refines the end in the background.
func markVideoExhausted(id int64) {
	if id <= 0 {
		return
	}
	now := time.Now()
	videoCooldownMu.Lock()
	if videoCooldown[id] <= now.Unix() {
		videoCooldown[id] = now.Add(videoCooldownDefault).Unix()
	}
	videoCooldownMu.Unlock()
	logf("[video] 账号 #%d 视频额度用完，新视频暂时跳过它", id)
	go func() {
		reset := videoResetLookup(id)
		if reset <= time.Now().Unix() || reset > time.Now().Add(videoCooldownMax).Unix() {
			return
		}
		videoCooldownMu.Lock()
		videoCooldown[id] = reset
		videoCooldownMu.Unlock()
		logf("[video] 账号 #%d 视频额度 %s 重置", id, time.Unix(reset, 0).Format("15:04"))
	}()
}

// videoCoolingAccounts returns the accounts still in their cooldown (expired ones are dropped).
func videoCoolingAccounts() map[int64]bool {
	now := time.Now().Unix()
	videoCooldownMu.Lock()
	defer videoCooldownMu.Unlock()
	out := map[int64]bool{}
	for id, until := range videoCooldown {
		if until <= now {
			delete(videoCooldown, id)
			continue
		}
		out[id] = true
	}
	return out
}

// videoPoolExhausted reports whether every enabled account is cooling down, and when the
// first of them resets.
func videoPoolExhausted() (bool, int64) {
	cooling := videoCoolingAccounts()
	var resets []int64
	enabled := 0
	videoCooldownMu.Lock()
	defer videoCooldownMu.Unlock()
	for _, a := range accountList() {
		if a.Status != "enabled" {
			continue
		}
		enabled++
		if !cooling[a.ID] {
			return false, 0
		}
		resets = append(resets, videoCooldown[a.ID])
	}
	if enabled == 0 {
		return false, 0
	}
	sort.Slice(resets, func(i, j int) bool { return resets[i] < resets[j] })
	return true, resets[0]
}

// videoPoolExhaustedError is the job error when no account can make a video right now.
func videoPoolExhaustedError(reset int64) string {
	return fmt.Sprintf("%s: every account is out of video quota; the first resets at %s (retry_after=%d)",
		errVideoQuota, time.Unix(reset, 0).Format("15:04"), retryAfterSeconds(reset))
}

func retryAfterSeconds(reset int64) int64 {
	if s := reset - time.Now().Unix(); s > 0 {
		return s
	}
	return 60
}

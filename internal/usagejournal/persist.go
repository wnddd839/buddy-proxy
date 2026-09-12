package usagejournal

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	snapshotVersion    = 1
	usageRetentionDays = 90
)

type dayBucketDisk struct {
	Requests         int64   `json:"requests"`
	Failed           int64   `json:"failed"`
	PromptTokens     int64   `json:"promptTokens"`
	CompletionTokens int64   `json:"completionTokens"`
	CachedTokens     int64   `json:"cachedTokens"`
	Credits          float64 `json:"credits"`
	CreditRows       int64   `json:"creditRows"`
}

type fileSnapshot struct {
	Version int                      `json:"version"`
	Entries []Entry                  `json:"entries"`
	ByDay   map[string]dayBucketDisk `json:"byDay"`
}

// Open loads or creates a journal persisted at path (empty path = in-memory only).
func Open(path string, capacity int) (*Journal, error) {
	j := New(capacity)
	path = strings.TrimSpace(path)
	if path == "" {
		return j, nil
	}
	j.path = path
	j.persist = true
	j.stopCh = make(chan struct{})
	j.wakeCh = make(chan struct{}, 1)
	j.doneCh = make(chan struct{})
	if err := j.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	go j.flushLoop()
	return j, nil
}

func (j *Journal) load() error {
	raw, err := os.ReadFile(j.path)
	if err != nil {
		return err
	}
	var snap fileSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return err
	}
	if snap.Version <= 0 {
		snap.Version = 1
	}
	j.entries = snap.Entries
	if len(j.entries) > j.capacity {
		j.entries = j.entries[len(j.entries)-j.capacity:]
	}
	j.byDay = map[string]dayBucket{}
	for day, b := range snap.ByDay {
		j.byDay[day] = dayBucket{
			Requests:         b.Requests,
			Failed:           b.Failed,
			PromptTokens:     b.PromptTokens,
			CompletionTokens: b.CompletionTokens,
			CachedTokens:     b.CachedTokens,
			Credits:          b.Credits,
			CreditRows:       b.CreditRows,
		}
	}
	j.pruneRetention(time.Now())
	return nil
}

func (j *Journal) markDirty() {
	if !j.persist {
		return
	}
	j.dirty = true
	j.kickFlush()
}

func (j *Journal) kickFlush() {
	if j.wakeCh == nil {
		return
	}
	select {
	case j.wakeCh <- struct{}{}:
	default:
	}
}

// Flush writes the journal to disk when dirty.
func (j *Journal) Flush() error {
	if j == nil || !j.persist {
		return nil
	}
	j.mu.Lock()
	if !j.dirty {
		j.mu.Unlock()
		return nil
	}
	snap := j.snapshotLocked()
	j.dirty = false
	j.mu.Unlock()
	if err := j.writeDisk(snap); err != nil {
		j.mu.Lock()
		j.dirty = true
		j.mu.Unlock()
		return err
	}
	return nil
}

// Close stops the flush loop and writes pending data.
func (j *Journal) Close() error {
	if j == nil || !j.persist {
		return nil
	}
	var err error
	j.closeOnce.Do(func() {
		close(j.stopCh)
		<-j.doneCh
		err = j.Flush()
	})
	return err
}

func (j *Journal) flushLoop() {
	defer close(j.doneCh)
	for {
		select {
		case <-j.stopCh:
			_ = j.Flush()
			return
		case <-j.wakeCh:
			timer := time.NewTimer(250 * time.Millisecond)
			select {
			case <-timer.C:
			case <-j.stopCh:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				_ = j.Flush()
				return
			}
			for {
				select {
				case <-j.wakeCh:
				default:
					goto flushed
				}
			}
		flushed:
			_ = j.Flush()
		}
	}
}

func (j *Journal) snapshotLocked() fileSnapshot {
	j.pruneRetention(time.Now())
	byDay := map[string]dayBucketDisk{}
	for day, b := range j.byDay {
		byDay[day] = dayBucketDisk{
			Requests:         b.Requests,
			Failed:           b.Failed,
			PromptTokens:     b.PromptTokens,
			CompletionTokens: b.CompletionTokens,
			CachedTokens:     b.CachedTokens,
			Credits:          b.Credits,
			CreditRows:       b.CreditRows,
		}
	}
	entries := append([]Entry(nil), j.entries...)
	return fileSnapshot{Version: snapshotVersion, Entries: entries, ByDay: byDay}
}

func (j *Journal) writeDisk(snap fileSnapshot) error {
	j.persistMu.Lock()
	defer j.persistMu.Unlock()
	if !j.dirReady {
		if err := os.MkdirAll(filepath.Dir(j.path), 0o700); err != nil {
			return err
		}
		j.dirReady = true
	}
	payload, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	tmp := j.path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, j.path)
}

func (j *Journal) pruneRetention(now time.Time) {
	if j == nil || len(j.byDay) == 0 {
		return
	}
	cutoff := now.AddDate(0, 0, -usageRetentionDays).Format("2006-01-02")
	for day := range j.byDay {
		if day < cutoff {
			delete(j.byDay, day)
		}
	}
}

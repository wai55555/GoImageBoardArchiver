package core

// Phase 6: スレッド URL 解決と増分判定のテスト。

import (
	"testing"
	"time"

	"GoImageBoardArchiver/internal/model"
)

func TestResolveThreadURL(t *testing.T) {
	t.Parallel()

	// ふたば: 相対パスは board URL と結合する (従来どおり)。
	u, err := resolveThreadURL("https://may.2chan.net/b/", "res/123456789.htm")
	if err != nil {
		t.Fatalf("resolveThreadURL(relative): %v", err)
	}
	if got, want := u.String(), "https://may.2chan.net/b/res/123456789.htm"; got != want {
		t.Errorf("relative: got %q, want %q", got, want)
	}

	// 4chan: 絶対 URL はそのまま使う。
	abs := "https://a.4cdn.org/vg/thread/1001.json"
	u, err = resolveThreadURL("https://boards.4chan.org/vg/", abs)
	if err != nil {
		t.Fatalf("resolveThreadURL(absolute): %v", err)
	}
	if u.String() != abs {
		t.Errorf("absolute: got %q, want %q", u.String(), abs)
	}
}

func TestNeedsUpdateWithLastModified(t *testing.T) {
	t.Parallel()

	if !NeedsUpdate(nil, 0, 0) {
		t.Fatal("スナップショットが無ければ更新が必要")
	}

	base := time.Unix(1700000000, 0)
	snap := &ThreadSnapshot{LastMediaCount: 3, LastModified: base}

	if NeedsUpdate(snap, 3, 1700000000) {
		t.Error("メディア数・更新時刻とも同じなら更新不要")
	}
	if !NeedsUpdate(snap, 4, 1700000000) {
		t.Error("メディア数が増えていれば更新が必要")
	}
	if !NeedsUpdate(snap, 3, 1700000001) {
		t.Error("last_modified が進んでいれば更新が必要")
	}
	if NeedsUpdate(snap, 3, 1699999999) {
		t.Error("last_modified が古い場合は更新不要")
	}

	complete := &ThreadSnapshot{IsComplete: true, LastModified: base}
	if NeedsUpdate(complete, 10, 1800000000) {
		t.Error("IsComplete のスレッドは更新不要")
	}
}

func TestSnapshotLastModified(t *testing.T) {
	t.Parallel()

	th := model.ThreadInfo{LastModified: 1700000100}
	if got := snapshotLastModified(th); got.Unix() != 1700000100 {
		t.Errorf("last_modified を使うべきです: %v", got)
	}
	if got := snapshotLastModified(model.ThreadInfo{}); time.Since(got) > time.Minute {
		t.Errorf("last_modified が無い場合は現在時刻を使うべきです: %v", got)
	}
}

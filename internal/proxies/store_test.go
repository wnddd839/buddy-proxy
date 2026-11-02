package proxies

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAddDedupAndMaskID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxies.json")
	file := Open(path)
	if _, err := file.Add("socks5h://u:p@127.0.0.1:1080", "warp"); err != nil {
		t.Fatal(err)
	}
	store, err := file.Add("socks5h://u:p@127.0.0.1:1080", "warp-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Proxies) != 1 {
		t.Fatalf("同 URL 应去重，实际 %d 条", len(store.Proxies))
	}
	if store.Proxies[0].Label != "warp-2" {
		t.Fatalf("label 未更新: %q", store.Proxies[0].Label)
	}
	if _, err := file.Add("ftp://x:1", ""); err == nil {
		t.Fatal("非法 scheme 应报错")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); runtime.GOOS != "windows" && perm != 0o600 {
		t.Fatalf("proxies.json 权限应为 600，实际 %o", perm)
	}

	id := ID("socks5h://u:p@127.0.0.1:1080")
	entry, ok, err := file.Find(id)
	if err != nil || !ok {
		t.Fatalf("按 ID 查找失败: ok=%v err=%v", ok, err)
	}
	if entry.URL != "socks5h://u:p@127.0.0.1:1080" {
		t.Fatalf("查到错误条目: %s", entry.URL)
	}
	if _, ok, _ := file.Find("deadbeef0000"); ok {
		t.Fatal("不存在的 ID 不该命中")
	}

	store, err = file.Remove(entry.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Proxies) != 0 {
		t.Fatalf("删除后应为空，实际 %d 条", len(store.Proxies))
	}
}

func TestReadMissingFileIsEmpty(t *testing.T) {
	file := Open(filepath.Join(t.TempDir(), "absent.json"))
	store, err := file.Read()
	if err != nil {
		t.Fatalf("文件不存在不该报错: %v", err)
	}
	if len(store.Proxies) != 0 {
		t.Fatalf("应为空清单，实际 %d 条", len(store.Proxies))
	}
}

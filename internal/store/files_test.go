package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"goclipboard/internal/model"
)

func tempFileStore(t *testing.T, opts ...FileOption) *FileStore {
	t.Helper()
	dir := t.TempDir()
	opts = append([]FileOption{WithFileRoot(dir)}, opts...)
	return NewFileStore(opts...)
}

func TestFileStorePutListGetDelete(t *testing.T) {
	fs := tempFileStore(t)

	f, err := fs.Put("room1", "hello.txt", "text/plain", []byte("hi"), time.Hour, "fp")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if f.ID == "" || f.Name != "hello.txt" || f.Size != 2 {
		t.Fatalf("unexpected file: %+v", f)
	}

	// Blob is on disk.
	bin := filepath.Join(fs.Root(), "room1", f.ID+".bin")
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("blob missing on disk: %v", err)
	}

	list := fs.List("room1")
	if len(list) != 1 || list[0].Name != "hello.txt" || list[0].Size != 2 {
		t.Fatalf("list = %+v", list)
	}

	got, ok := fs.Get("room1", f.ID)
	if !ok || string(got.Data) != "hi" {
		t.Fatalf("Get: ok=%v data=%q", ok, got.Data)
	}

	if !fs.Delete("room1", f.ID) {
		t.Fatal("Delete should return true")
	}
	if _, ok := fs.Get("room1", f.ID); ok {
		t.Fatal("file should be gone")
	}
	if len(fs.List("room1")) != 0 {
		t.Fatal("list should be empty")
	}
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Fatalf("blob should be removed, err=%v", err)
	}
}

func TestFileStoreExpire(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	fs := tempFileStore(t, WithFileClock(func() time.Time { return now }))

	f, err := fs.Put("r", "a.bin", "application/octet-stream", []byte("x"), time.Second, "fp")
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(2 * time.Second)
	if _, ok := fs.Get("r", f.ID); ok {
		t.Fatal("expired file should not be gettable")
	}
}

func TestFileStoreNoSizeLimits(t *testing.T) {
	fs := tempFileStore(t)
	// Former "too large" payload must be accepted.
	data := make([]byte, 64<<10) // 64 KiB
	for i := range data {
		data[i] = byte(i)
	}
	f, err := fs.Put("r", "big.bin", "application/octet-stream", data, time.Hour, "fp")
	if err != nil {
		t.Fatalf("large put: %v", err)
	}
	got, ok := fs.Get("r", f.ID)
	if !ok || int64(len(got.Data)) != int64(len(data)) {
		t.Fatalf("get size = %d ok=%v", len(got.Data), ok)
	}
}

func TestStartupSweepRemovesOrphans(t *testing.T) {
	dir := t.TempDir()
	room := filepath.Join(dir, "sweeproom")
	if err := os.MkdirAll(room, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	meta := func(id string) string {
		return `{"id":"` + id + `","name":"f.txt","contentType":"text/plain","size":4,` +
			`"ttlSeconds":3600,"expiresAt":"` + now.Add(time.Hour).UTC().Format(time.RFC3339) +
			`","uploadedAt":"` + now.UTC().Format(time.RFC3339) + `"}`
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(room, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Valid pair: survives the sweep.
	write("aaaaaaaa.meta.json", meta("aaaaaaaa"))
	write("aaaaaaaa.bin", "data")
	// Orphans left by a crash between the bin/meta rename pair in PutReader.
	write("bbbbbbbb.bin", "orphan")               // blob without meta
	write("cccccccc.bin.tmp", "partial")          // temp file
	write("dddddddd.meta.json", meta("dddddddd")) // meta without blob
	write("eeeeeeee.meta.json.tmp", `{"broken`)   // interrupted meta write

	s := NewFileStore(WithFileRoot(dir))

	if _, ok := s.Get("sweeproom", "aaaaaaaa"); !ok {
		t.Fatal("valid file should survive the sweep")
	}
	for _, gone := range []string{
		"bbbbbbbb.bin", "cccccccc.bin.tmp", "dddddddd.meta.json", "eeeeeeee.meta.json.tmp",
	} {
		if _, err := os.Stat(filepath.Join(room, gone)); !os.IsNotExist(err) {
			t.Fatalf("%s should have been removed, err=%v", gone, err)
		}
	}
}

func TestSanitizePathInName(t *testing.T) {
	fs := tempFileStore(t)
	f, err := fs.Put("r", "../../etc/passwd", "text/plain", []byte("x"), time.Hour, "fp")
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "passwd" {
		t.Fatalf("name = %q, want passwd", f.Name)
	}
}

func TestFileStoreRevisionBumps(t *testing.T) {
	fs := tempFileStore(t)
	_, rev0 := fs.ListWithRevision("r")
	if rev0 != 0 {
		t.Fatalf("initial rev = %d", rev0)
	}
	f, err := fs.Put("r", "a.txt", "text/plain", []byte("a"), time.Hour, "fp")
	if err != nil {
		t.Fatal(err)
	}
	_, rev1 := fs.ListWithRevision("r")
	if rev1 <= rev0 {
		t.Fatalf("rev after put = %d, want > %d", rev1, rev0)
	}
	if !fs.Delete("r", f.ID) {
		t.Fatal("delete failed")
	}
	_, rev2 := fs.ListWithRevision("r")
	if rev2 <= rev1 {
		t.Fatalf("rev after delete = %d, want > %d", rev2, rev1)
	}
}

func TestFileStoreReloadFromDisk(t *testing.T) {
	dir := t.TempDir()
	fs1 := NewFileStore(WithFileRoot(dir))
	f, err := fs1.Put("roomx", "persist.txt", "text/plain", []byte("kept"), time.Hour, "fp")
	if err != nil {
		t.Fatal(err)
	}

	// New store instance over same directory reloads index.
	fs2 := NewFileStore(WithFileRoot(dir))
	list := fs2.List("roomx")
	if len(list) != 1 || list[0].ID != f.ID || list[0].Name != "persist.txt" {
		t.Fatalf("reloaded list = %+v", list)
	}
	got, ok := fs2.Get("roomx", f.ID)
	if !ok || string(got.Data) != "kept" {
		t.Fatalf("reloaded get: ok=%v data=%q", ok, got.Data)
	}
}

func TestFileStoreOpenStream(t *testing.T) {
	fs := tempFileStore(t)
	f, err := fs.Put("r", "s.txt", "text/plain", []byte("stream-me"), time.Hour, "fp")
	if err != nil {
		t.Fatal(err)
	}
	meta, rc, err := fs.Open("r", f.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if meta.Size != 9 || meta.Name != "s.txt" {
		t.Fatalf("meta = %+v", meta)
	}
	buf := make([]byte, 32)
	n, _ := rc.Read(buf)
	if string(buf[:n]) != "stream-me" {
		t.Fatalf("read %q", buf[:n])
	}
}

func TestEmptyFileRejected(t *testing.T) {
	fs := tempFileStore(t)
	if _, err := fs.Put("r", "e.txt", "text/plain", []byte{}, time.Hour, "fp"); err != model.ErrEmptyFile {
		t.Fatalf("want ErrEmptyFile, got %v", err)
	}
}

func TestFilePasswordRequiredAndChecked(t *testing.T) {
	fs := tempFileStore(t)
	if _, err := fs.Put("r", "a.txt", "text/plain", []byte("x"), time.Hour, ""); err == nil {
		t.Fatal("empty file password should fail")
	}
	f, err := fs.Put("r", "a.txt", "text/plain", []byte("secret-data"), time.Hour, "dl-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.CheckFilePassword("r", f.ID, "wrong"); err != ErrBadFilePassword {
		t.Fatalf("want ErrBadFilePassword, got %v", err)
	}
	if err := fs.CheckFilePassword("r", f.ID, "dl-secret"); err != nil {
		t.Fatalf("correct password: %v", err)
	}
	// Password hash reloads from disk.
	fs2 := NewFileStore(WithFileRoot(fs.Root()))
	if err := fs2.CheckFilePassword("r", f.ID, "dl-secret"); err != nil {
		t.Fatalf("reloaded check: %v", err)
	}
}

func TestRoomFileUploadToggle(t *testing.T) {
	fs := tempFileStore(t)
	if fs.IsFileUploadEnabled("r1") {
		t.Fatal("default should be disabled")
	}
	if err := fs.SetFileUploadEnabled("r1", true); err != nil {
		t.Fatal(err)
	}
	if !fs.IsFileUploadEnabled("r1") {
		t.Fatal("expected enabled")
	}
	// Reload from disk
	fs2 := NewFileStore(WithFileRoot(fs.Root()))
	if !fs2.IsFileUploadEnabled("r1") {
		t.Fatal("expected enabled after reload")
	}
	if err := fs2.SetFileUploadEnabled("r1", false); err != nil {
		t.Fatal(err)
	}
	if fs2.IsFileUploadEnabled("r1") {
		t.Fatal("expected disabled")
	}
	// DeleteRoom clears settings
	if err := fs2.SetFileUploadEnabled("r1", true); err != nil {
		t.Fatal(err)
	}
	fs2.DeleteRoom("r1")
	if fs2.IsFileUploadEnabled("r1") {
		t.Fatal("settings should be cleared with room")
	}
}

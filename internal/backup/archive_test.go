package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestArchive_CreateVerifyRestoreCompleteVolume(t *testing.T) {
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		t.Skip("restore assigns bot UID 65532 and requires root on Unix")
	}
	source, destination, restored := t.TempDir(), t.TempDir(), t.TempDir()
	files := map[string]string{"bot.db": "test database", "bot.db-wal": "test wal", "bot.db-shm": "test shm", "nested/extra": "test extra"}
	for name, data := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(source, name)), 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	name, err := Create(t.Context(), source, destination)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, ".tar.gz") || strings.Contains(name, "partial") {
		t.Fatalf("invalid published name: %s", name)
	}
	if err := Verify(t.Context(), name); err != nil {
		t.Fatal(err)
	}
	if err := Restore(t.Context(), name, restored); err != nil {
		t.Fatal(err)
	}
	for file, want := range files {
		for _, dir := range []string{source, restored} {
			data, err := os.ReadFile(filepath.Join(dir, file))
			if err != nil || string(data) != want {
				t.Fatalf("file %s: %q, %v", file, data, err)
			}
		}
	}
	if err := Restore(t.Context(), name, restored); err == nil {
		t.Fatal("restore overwrote nonempty destination")
	}
	entries, err := os.ReadDir(destination)
	if err != nil || len(entries) != 1 {
		t.Fatalf("unexpected incomplete archive: %v %v", entries, err)
	}
}

func TestArchive_RejectsCorruptionAndUnsafePaths(t *testing.T) {
	for _, entry := range []string{"../bot.db", "/bot.db", "sub/../bot.db", "bot.db", "symlink"} {
		t.Run(entry, func(t *testing.T) {
			name := filepath.Join(t.TempDir(), "test.tar.gz")
			f, err := os.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			gz := gzip.NewWriter(f)
			tw := tar.NewWriter(gz)
			h := &tar.Header{Name: entry, Mode: 0600, Size: 4, Typeflag: tar.TypeReg}
			if entry == "symlink" {
				h.Typeflag, h.Size, h.Linkname = tar.TypeSymlink, 0, "../outside"
			}
			if err := tw.WriteHeader(h); err != nil {
				t.Fatal(err)
			}
			if h.Size > 0 {
				if _, err := tw.Write([]byte("test")); err != nil {
					t.Fatal(err)
				}
			}
			for _, err := range []error{tw.Close(), gz.Close(), f.Close()} {
				if err != nil {
					t.Fatal(err)
				}
			}
			if entry == "bot.db" {
				data, err := os.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				data[len(data)-8] ^= 0xff // Corrupt gzip CRC after a complete tar payload.
				if err := os.WriteFile(name, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := Verify(t.Context(), name); err == nil {
				t.Fatal("invalid archive accepted")
			}
			out := t.TempDir()
			if err := Restore(t.Context(), name, out); err == nil {
				t.Fatal("invalid archive restored")
			}
			entries, err := os.ReadDir(out)
			if err != nil || len(entries) != 0 {
				t.Fatal("verification failure modified destination")
			}
		})
	}
}

func TestArchive_RejectsWrongVolumeAndNestedDestination(t *testing.T) {
	source := t.TempDir()
	if _, err := Create(t.Context(), source, t.TempDir()); err == nil {
		t.Fatal("empty volume accepted")
	}
	if err := os.WriteFile(filepath.Join(source, "bot.db"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(t.Context(), source, filepath.Join(source, "backups")); err == nil {
		t.Fatal("recursive backup accepted")
	}
	entries, err := os.ReadDir(source)
	if err != nil || len(entries) != 1 {
		t.Fatal("backup modified source directory")
	}
}

func TestArchive_CanceledCopyDoesNotPublishPartialFile(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "bot.db"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Create(ctx, source, destination); err == nil {
		t.Fatal("canceled backup succeeded")
	}
	entries, err := os.ReadDir(destination)
	if err != nil || len(entries) != 0 {
		t.Fatal("failed backup published a file")
	}
}

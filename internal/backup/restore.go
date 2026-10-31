package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

func Verify(ctx context.Context, archive string) error {
	f, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	err = readArchive(ctx, f, nil)
	return errors.Join(err, f.Close())
}

func Restore(ctx context.Context, archive, destination string) (err error) {
	f, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	if err := readArchive(ctx, f, nil); err != nil {
		return fmt.Errorf("verify before restore: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind archive: %w", err)
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		return fmt.Errorf("read restore destination: %w", err)
	}
	if len(entries) != 0 {
		return errors.New("restore destination must be empty; use a new Docker volume")
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		return fmt.Errorf("open restore root: %w", err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	if err := readArchive(ctx, f, func(h *tar.Header, in io.Reader) error {
		return extract(ctx, root, h, in)
	}); err != nil {
		return fmt.Errorf("restore incomplete (do not start bot on this volume): %w", err)
	}
	return setOwner(ctx, destination)
}

func readArchive(ctx context.Context, in io.Reader, visit func(*tar.Header, io.Reader) error) (err error) {
	gz, err := gzip.NewReader(contextReader{ctx, in})
	if err != nil {
		return fmt.Errorf("read gzip: %w", err)
	}
	defer func() { err = errors.Join(err, gz.Close()) }()
	tr := tar.NewReader(gz)
	seen := make(map[string]bool)
	database := false
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		name := strings.TrimSuffix(h.Name, "/")
		if !filepath.IsLocal(filepath.FromSlash(name)) || path.Clean(name) != name || strings.ContainsAny(name, "\\:") || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir) || seen[name] {
			return errors.New("archive has unsafe, unsupported or duplicate entry")
		}
		seen[name] = true
		database = database || (name == "bot.db" && h.Typeflag == tar.TypeReg && h.Size > 0)
		if visit != nil {
			if err := visit(h, tr); err != nil {
				return err
			}
		}
		if _, err := io.Copy(io.Discard, tr); err != nil {
			return fmt.Errorf("read archive entry: %w", err)
		}
	}
	// tar EOF precedes gzip's checksum; drain the stream to validate its trailer.
	_, err = io.Copy(io.Discard, gz)
	if err != nil {
		return fmt.Errorf("validate gzip checksum: %w", err)
	}
	if !database {
		return errors.New("archive does not contain a nonempty bot.db")
	}
	return nil
}

func extract(ctx context.Context, root *os.Root, h *tar.Header, in io.Reader) error {
	if h.Typeflag == tar.TypeDir {
		return root.MkdirAll(h.Name, 0750)
	}
	if err := root.MkdirAll(filepath.Dir(h.Name), 0750); err != nil {
		return err
	}
	f, err := root.OpenFile(h.Name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, contextReader{ctx, in})
	return errors.Join(err, f.Sync(), f.Close())
}

func setOwner(ctx context.Context, destination string) error {
	return filepath.WalkDir(destination, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		mode := os.FileMode(0600)
		if entry.IsDir() {
			mode = 0750
		}
		if err := os.Chmod(name, mode); err != nil {
			return err
		}
		if runtime.GOOS != "windows" {
			return os.Chown(name, 65532, 65532)
		}
		return nil
	})
}

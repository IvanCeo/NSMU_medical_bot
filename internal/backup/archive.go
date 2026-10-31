package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Create archives a stopped bot's complete data directory without opening SQLite.
func Create(ctx context.Context, source, destination string) (name string, err error) {
	source, destination, err = archiveDirectories(source, destination)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp(destination, ".backup-*.partial")
	if err != nil {
		return "", fmt.Errorf("create temporary archive: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(f.Name()))
		}
	}()
	err = writeArchive(ctx, f, source)
	err = errors.Join(err, f.Sync(), f.Close())
	if err != nil {
		return "", fmt.Errorf("write archive: %w", err)
	}
	suffix := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f.Name()), ".backup-"), ".partial")
	name = filepath.Join(destination, "bot-"+time.Now().UTC().Format("20060102T150405Z")+"-"+suffix+".tar.gz")
	if err = os.Rename(f.Name(), name); err != nil {
		return "", fmt.Errorf("publish archive: %w", err)
	}
	return name, nil
}

func archiveDirectories(source, destination string) (string, string, error) {
	source, err := filepath.Abs(source)
	if err != nil {
		return "", "", err
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return "", "", fmt.Errorf("resolve source: %w", err)
	}
	info, err := os.Lstat(filepath.Join(source, "bot.db"))
	if err != nil {
		return "", "", fmt.Errorf("find bot.db (check data volume): %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return "", "", errors.New("bot.db must be a nonempty regular file")
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return "", "", err
	}
	if insideDirectory(source, destination) {
		return "", "", errors.New("backup destination must be outside source")
	}
	if err := os.MkdirAll(destination, 0700); err != nil {
		return "", "", fmt.Errorf("create backup directory: %w", err)
	}
	destination, err = filepath.EvalSymlinks(destination)
	if err != nil {
		return "", "", fmt.Errorf("resolve destination: %w", err)
	}
	if insideDirectory(source, destination) {
		return "", "", errors.New("backup destination must be outside source")
	}
	return source, destination, nil
}

func insideDirectory(parent, name string) bool {
	rel, err := filepath.Rel(parent, name)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

func writeArchive(ctx context.Context, output io.Writer, source string) error {
	gz := gzip.NewWriter(output)
	tw := tar.NewWriter(gz)
	err := filepath.WalkDir(source, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if name == source {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported data file: %s", entry.Name())
		}
		rel, err := filepath.Rel(source, name)
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(header); err != nil || info.IsDir() {
			return err
		}
		return copyFile(ctx, tw, name, info.Size())
	})
	return errors.Join(err, tw.Close(), gz.Close())
}

func copyFile(ctx context.Context, out io.Writer, name string, size int64) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, contextReader{ctx, f})
	if err == nil && n != size {
		err = errors.New("data file changed during backup; stop bot before copying")
	}
	return errors.Join(err, f.Close())
}

type contextReader struct {
	ctx context.Context
	in  io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.in.Read(p)
}

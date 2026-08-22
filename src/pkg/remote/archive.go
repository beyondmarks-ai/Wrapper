package remote

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/klauspost/compress/zstd"
)

var (
	ErrTransferTooLarge = errors.New("transfer exceeds the 20 GB limit")
	ErrUnsafePath       = errors.New("archive contains an unsafe path")
	ErrUnsupportedEntry = errors.New("symlinks, reparse points, and special files are not supported")
	ErrIntegrity        = errors.New("transfer integrity verification failed")
)

type ConflictPolicy string

const (
	ConflictKeepBoth  ConflictPolicy = "keep_both"
	ConflictOverwrite ConflictPolicy = "overwrite"
	ConflictSkip      ConflictPolicy = "skip"
)

type scannedEntry struct {
	sourcePath  string
	archivePath string
	info        fs.FileInfo
}

func BuildEncryptedPayload(ctx context.Context, transferID string, paths []string, recipient string,
	destination io.Writer,
) (Manifest, error) {
	entries, total, err := scanTransferPaths(ctx, paths)
	if err != nil {
		return Manifest{}, err
	}
	parsedRecipient, err := age.ParseX25519Recipient(recipient)
	if err != nil {
		return Manifest{}, fmt.Errorf("parse destination encryption key: %w", err)
	}
	ageWriter, err := age.Encrypt(destination, parsedRecipient)
	if err != nil {
		return Manifest{}, fmt.Errorf("start payload encryption: %w", err)
	}
	zstdWriter, err := zstd.NewWriter(ageWriter, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return Manifest{}, fmt.Errorf("start payload compression: %w", err)
	}
	tarWriter := tar.NewWriter(zstdWriter)
	manifest := Manifest{TransferID: transferID, Archive: true, PlainSize: total}
	if len(entries) > 0 {
		manifest.Name = filepath.Base(entries[0].archivePath)
	}

	for _, entry := range entries {
		if err = ctx.Err(); err != nil {
			return Manifest{}, err
		}
		header, headerErr := tar.FileInfoHeader(entry.info, "")
		if headerErr != nil {
			return Manifest{}, fmt.Errorf("create archive header for %s: %w", entry.sourcePath, headerErr)
		}
		header.Name = filepath.ToSlash(entry.archivePath)
		header.ModTime = header.ModTime.UTC().Truncate(time.Second)
		if err = tarWriter.WriteHeader(header); err != nil {
			return Manifest{}, fmt.Errorf("write archive header for %s: %w", entry.sourcePath, err)
		}
		item := ManifestItem{
			Path: header.Name, Size: header.Size, Mode: uint32(header.FileInfo().Mode().Perm()),
			Modified: header.ModTime.UTC(), IsDir: header.FileInfo().IsDir(),
		}
		if entry.info.Mode().IsRegular() {
			file, openErr := os.Open(entry.sourcePath)
			if openErr != nil {
				return Manifest{}, fmt.Errorf("open %s: %w", entry.sourcePath, openErr)
			}
			hash := sha256.New()
			_, copyErr := copyWithContext(ctx, io.MultiWriter(tarWriter, hash), file)
			closeErr := file.Close()
			if copyErr != nil {
				return Manifest{}, fmt.Errorf("archive %s: %w", entry.sourcePath, copyErr)
			}
			if closeErr != nil {
				return Manifest{}, fmt.Errorf("close %s: %w", entry.sourcePath, closeErr)
			}
			item.SHA256 = hex.EncodeToString(hash.Sum(nil))
		}
		manifest.Entries = append(manifest.Entries, item)
	}
	if err = tarWriter.Close(); err != nil {
		return Manifest{}, fmt.Errorf("finish archive: %w", err)
	}
	if err = zstdWriter.Close(); err != nil {
		return Manifest{}, fmt.Errorf("finish compression: %w", err)
	}
	if err = ageWriter.Close(); err != nil {
		return Manifest{}, fmt.Errorf("finish encryption: %w", err)
	}
	manifest.SHA256, err = manifestDigest(manifest.Entries)
	return manifest, err
}

func ExtractEncryptedPayload(ctx context.Context, identity Identity, source io.Reader, destination string,
	expected Manifest, conflict ConflictPolicy,
) error {
	ageIdentity, err := age.ParseX25519Identity(identity.AgeIdentity)
	if err != nil {
		return fmt.Errorf("parse device encryption identity: %w", err)
	}
	decrypted, err := age.Decrypt(source, ageIdentity)
	if err != nil {
		return fmt.Errorf("decrypt transfer: %w", err)
	}
	zstdReader, err := zstd.NewReader(decrypted)
	if err != nil {
		return fmt.Errorf("open compressed transfer: %w", err)
	}
	defer zstdReader.Close()
	if err = os.MkdirAll(destination, 0o755); err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	staging, err := os.MkdirTemp(destination, ".wrapper-transfer-")
	if err != nil {
		return fmt.Errorf("create transfer staging directory: %w", err)
	}
	defer os.RemoveAll(staging) //nolint:errcheck // Best-effort cleanup after commit or failure.

	reader := tar.NewReader(zstdReader)
	actual := make([]ManifestItem, 0, len(expected.Entries))
	var total int64
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return fmt.Errorf("read transfer archive: %w", nextErr)
		}
		target, safeErr := safeArchiveTarget(staging, header.Name)
		if safeErr != nil {
			return safeErr
		}
		item := ManifestItem{
			Path: filepath.ToSlash(header.Name), Size: header.Size, Mode: uint32(header.FileInfo().Mode().Perm()),
			Modified: header.ModTime.UTC(), IsDir: header.FileInfo().IsDir(),
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err = os.MkdirAll(target, header.FileInfo().Mode().Perm()); err != nil {
				return fmt.Errorf("create transferred directory: %w", err)
			}
		case tar.TypeReg, tar.TypeRegA:
			total += header.Size
			if total > MaxTransferSize {
				return ErrTransferTooLarge
			}
			if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create transferred parent directory: %w", err)
			}
			file, createErr := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, header.FileInfo().Mode().Perm())
			if createErr != nil {
				return fmt.Errorf("create transferred file: %w", createErr)
			}
			hash := sha256.New()
			_, copyErr := copyWithContext(ctx, io.MultiWriter(file, hash), io.LimitReader(reader, header.Size))
			closeErr := file.Close()
			if copyErr != nil {
				return fmt.Errorf("extract transferred file: %w", copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close transferred file: %w", closeErr)
			}
			item.SHA256 = hex.EncodeToString(hash.Sum(nil))
			_ = os.Chtimes(target, header.AccessTime, header.ModTime)
		default:
			return fmt.Errorf("%w: %s", ErrUnsupportedEntry, header.Name)
		}
		actual = append(actual, item)
	}
	actualDigest, err := manifestDigest(actual)
	if err != nil || actualDigest != expected.SHA256 {
		return ErrIntegrity
	}
	return commitStaging(staging, destination, conflict)
}

func scanTransferPaths(ctx context.Context, paths []string) ([]scannedEntry, int64, error) {
	if len(paths) == 0 {
		return nil, 0, errors.New("no files or folders selected")
	}
	seenRoots := make(map[string]struct{}, len(paths))
	var entries []scannedEntry
	var total int64
	for _, root := range paths {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return nil, 0, fmt.Errorf("resolve selected path: %w", err)
		}
		base := filepath.Base(filepath.Clean(absolute))
		folded := strings.ToLower(base)
		if _, exists := seenRoots[folded]; exists {
			return nil, 0, fmt.Errorf("selected items have the same top-level name: %s", base)
		}
		seenRoots[folded] = struct{}{}
		err = filepath.Walk(absolute, func(path string, info fs.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 || (!info.Mode().IsRegular() && !info.IsDir()) {
				return fmt.Errorf("%w: %s", ErrUnsupportedEntry, path)
			}
			relative, relErr := filepath.Rel(absolute, path)
			if relErr != nil {
				return relErr
			}
			archivePath := base
			if relative != "." {
				archivePath = filepath.Join(base, relative)
			}
			if info.Mode().IsRegular() {
				total += info.Size()
				if total > MaxTransferSize {
					return ErrTransferTooLarge
				}
			}
			entries = append(entries, scannedEntry{sourcePath: path, archivePath: archivePath, info: info})
			return nil
		})
		if err != nil {
			return nil, 0, err
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].archivePath < entries[j].archivePath })
	return entries, total, nil
}

func safeArchiveTarget(root, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) ||
		!safePlatformArchivePath(clean) {
		return "", fmt.Errorf("%w: %s", ErrUnsafePath, name)
	}
	target := filepath.Join(root, clean)
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %s", ErrUnsafePath, name)
	}
	return target, nil
}

func manifestDigest(entries []ManifestItem) (string, error) {
	encoded, err := json.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("encode transfer manifest: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func copyWithContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 1024*1024)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			writeCount, writeErr := destination.Write(buffer[:count])
			written += int64(writeCount)
			if writeErr != nil {
				return written, writeErr
			}
			if writeCount != count {
				return written, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return written, nil
		}
		if readErr != nil {
			return written, readErr
		}
	}
}

func commitStaging(staging, destination string, conflict ConflictPolicy) error {
	entries, err := os.ReadDir(staging)
	if err != nil {
		return fmt.Errorf("read transfer staging directory: %w", err)
	}
	for _, entry := range entries {
		source := filepath.Join(staging, entry.Name())
		target := filepath.Join(destination, entry.Name())
		if _, statErr := os.Stat(target); statErr == nil {
			switch conflict {
			case ConflictSkip:
				continue
			case ConflictOverwrite:
				if err = os.RemoveAll(target); err != nil {
					return fmt.Errorf("replace existing destination: %w", err)
				}
			case ConflictKeepBoth:
				target = availableName(target)
			default:
				return fmt.Errorf("unknown conflict policy %q", conflict)
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		if err = os.Rename(source, target); err != nil {
			return fmt.Errorf("commit transferred item: %w", err)
		}
	}
	return nil
}

func availableName(path string) string {
	directory, name := filepath.Split(path)
	extension := filepath.Ext(name)
	stem := strings.TrimSuffix(name, extension)
	for index := 1; ; index++ {
		candidate := filepath.Join(directory, fmt.Sprintf("%s (%d)%s", stem, index, extension))
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
}

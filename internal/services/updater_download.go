package services

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// downloadFile 下载 url 到 dest(覆盖)。父目录会自动创建。
func downloadFile(ctx context.Context, client *http.Client, url, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("download %s: status %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

// sha256File 返回文件的 hex 小写 sha256。
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// parseChecksumsReader 解析 `<sha256>  <filename>` 每行一条的 checksums.txt。
// 文件名去掉可能的 `*` 前缀和路径部分,只取 basename。
func parseChecksumsReader(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		sum := strings.ToLower(fields[0])
		name := filepath.Base(strings.TrimPrefix(fields[len(fields)-1], "*"))
		out[name] = sum
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// parseChecksumsFile 从本地 checksums.txt 文件解析。
func parseChecksumsFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseChecksumsReader(f)
}

// extractTarGz 把 .tar.gz 归档解压到 destDir。拒绝跳出根目录的条目(zip-slip 防护)。
func extractTarGz(archive, destDir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	return extractTarStream(gz, destDir)
}

func extractTarStream(r io.Reader, destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(absDest, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)&0o755|0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		}
	}
}

// extractZip 把 .zip 归档解压到 destDir。拒绝跳出根目录的条目。
func extractZip(archive, destDir string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}
	for _, f := range r.File {
		target, err := safeJoin(absDest, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		src, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode()&0o777)
		if err != nil {
			src.Close()
			return err
		}
		if _, err := io.Copy(out, src); err != nil {
			src.Close()
			out.Close()
			return err
		}
		src.Close()
		out.Close()
	}
	return nil
}

// safeJoin 把 name 拼到 base 下,并保证结果仍在 base 内(防 ../ 逃逸)。
func safeJoin(base, name string) (string, error) {
	cleaned := filepath.Clean(name)
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("unsafe archive entry: %s", name)
	}
	target := filepath.Join(base, cleaned)
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absTarget+string(os.PathSeparator), base+string(os.PathSeparator)) && absTarget != base {
		return "", fmt.Errorf("unsafe archive entry: %s", name)
	}
	return absTarget, nil
}

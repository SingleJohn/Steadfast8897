package source

import (
	"bytes"
	"io"
	"mime"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/unicode"
)

var (
	cspMetaCharsetPattern     = regexp.MustCompile(`(?is)<meta[^>]+charset\s*=\s*["']?\s*([A-Za-z0-9._-]+)`)
	cspMetaContentTypePattern = regexp.MustCompile(`(?is)<meta[^>]+http-equiv\s*=\s*["']?content-type["']?[^>]+content\s*=\s*["'][^"']*charset\s*=\s*([A-Za-z0-9._-]+)`)
)

func decodeCSPHTTPText(raw []byte, contentType string) (string, string, bool) {
	if len(raw) == 0 {
		return "", "utf-8", isCSPTextContentType(contentType)
	}
	if !isCSPTextContentType(contentType) && !looksLikeCSPText(raw) {
		return "", "", false
	}
	if utf8.Valid(raw) && charsetFromContentType(contentType) == "" && charsetFromHTMLMeta(raw) == "" && !hasCSPBOM(raw) {
		return string(raw), "utf-8", true
	}
	enc, name := detectCSPHTTPEncoding(raw, contentType)
	if enc == nil {
		if utf8.Valid(raw) {
			return string(raw), "utf-8", true
		}
		return "", "", false
	}
	reader := enc.NewDecoder().Reader(bytes.NewReader(raw))
	data, err := io.ReadAll(io.LimitReader(reader, int64(len(raw))*4+4096))
	if err != nil {
		return "", "", false
	}
	return string(data), cspDefaultString(name, "utf-8"), true
}

func detectCSPHTTPEncoding(raw []byte, contentType string) (encoding.Encoding, string) {
	if name := charsetFromContentType(contentType); name != "" {
		if enc, canonical := charset.Lookup(name); enc != nil {
			return enc, canonical
		}
	}
	if name := charsetFromHTMLMeta(raw); name != "" {
		if enc, canonical := charset.Lookup(name); enc != nil {
			return enc, canonical
		}
	}
	if enc, name := charsetFromBOM(raw); enc != nil {
		return enc, name
	}
	enc, name, _ := charset.DetermineEncoding(raw, contentType)
	return enc, name
}

func charsetFromContentType(contentType string) string {
	_, params, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err == nil {
		return strings.TrimSpace(params["charset"])
	}
	for _, part := range strings.Split(contentType, ";") {
		part = strings.TrimSpace(part)
		if key, value, ok := strings.Cut(part, "="); ok && strings.EqualFold(strings.TrimSpace(key), "charset") {
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return ""
}

func charsetFromHTMLMeta(raw []byte) string {
	head := raw
	if len(head) > 8192 {
		head = head[:8192]
	}
	if match := cspMetaCharsetPattern.FindSubmatch(head); len(match) == 2 {
		return strings.TrimSpace(string(match[1]))
	}
	if match := cspMetaContentTypePattern.FindSubmatch(head); len(match) == 2 {
		return strings.TrimSpace(string(match[1]))
	}
	return ""
}

func charsetFromBOM(raw []byte) (encoding.Encoding, string) {
	switch {
	case bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}):
		return unicode.UTF8, "utf-8"
	case bytes.HasPrefix(raw, []byte{0xFE, 0xFF}):
		return unicode.UTF16(unicode.BigEndian, unicode.ExpectBOM), "utf-16be"
	case bytes.HasPrefix(raw, []byte{0xFF, 0xFE}):
		return unicode.UTF16(unicode.LittleEndian, unicode.ExpectBOM), "utf-16le"
	default:
		return nil, ""
	}
}

func hasCSPBOM(raw []byte) bool {
	enc, _ := charsetFromBOM(raw)
	return enc != nil
}

func isCSPTextContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	mediaType = strings.ToLower(mediaType)
	if strings.HasPrefix(mediaType, "text/") {
		return true
	}
	switch mediaType {
	case "application/json", "application/xml", "application/xhtml+xml", "application/javascript", "application/x-javascript":
		return true
	}
	return strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml")
}

func looksLikeCSPText(raw []byte) bool {
	head := bytes.TrimSpace(raw)
	if len(head) == 0 {
		return true
	}
	if len(head) > 512 {
		head = head[:512]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return false
	}
	lower := bytes.ToLower(head)
	return bytes.HasPrefix(lower, []byte("<!doctype")) ||
		bytes.HasPrefix(lower, []byte("<html")) ||
		bytes.HasPrefix(lower, []byte("<?xml")) ||
		bytes.HasPrefix(lower, []byte("{")) ||
		bytes.HasPrefix(lower, []byte("["))
}

package gateway

import (
	"encoding/base64"
	"io"
	"path"
	"strings"

	"github.com/pkg/sftp"
)

const previewLimit = 32 << 20

var previewTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".bmp": "image/bmp", ".ico": "image/x-icon", ".svg": "image/svg+xml", ".avif": "image/avif",
	".pdf": "application/pdf", ".mp3": "audio/mpeg", ".wav": "audio/wav", ".ogg": "audio/ogg", ".flac": "audio/flac", ".m4a": "audio/mp4", ".mp4": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime",
}

func readPreview(c *sftp.Client, filename string) (map[string]any, error) {
	mediaType := previewTypes[strings.ToLower(path.Ext(filename))]
	if mediaType == "" {
		return nil, fail(422, "This file type cannot be previewed. Download it to view.")
	}
	f, err := c.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fail(422, "仅支持查看普通文件")
	}
	if st.Size() > previewLimit {
		return nil, fail(413, "Preview is limited to 32 MiB. Download this file to view.")
	}
	data, err := io.ReadAll(io.LimitReader(f, previewLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > previewLimit {
		return nil, fail(413, "Preview is limited to 32 MiB. Download this file to view.")
	}
	return map[string]any{"path": filename, "mime": mediaType, "data": base64.StdEncoding.EncodeToString(data)}, nil
}

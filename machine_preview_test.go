package gateway

import (
	"encoding/base64"
	"os"
	"path"
	"testing"
)

func TestMachinePreviewFormatsAndLimits(t *testing.T) {
	f := newWebFixture(t)
	machineFile(t, f, map[string]string{"op": "preview", "path": "/image.png"}, 401)
	f.login(t)
	root := machineFile(t, f, map[string]string{"op": "list"}, 200)["path"].(string)
	for _, name := range []string{"image.PNG", "document.pdf", "sound.mp3", "clip.mp4"} {
		p := path.Join(root, name)
		data := []byte("preview fixture\x00binary")
		if err := os.WriteFile(localSFTPFixturePath(p), data, 0600); err != nil {
			t.Fatal(err)
		}
		got := machineFile(t, f, map[string]string{"op": "preview", "path": p}, 200)
		decoded, err := base64.StdEncoding.DecodeString(got["data"].(string))
		if err != nil || string(decoded) != string(data) {
			t.Fatal("Preview changed binary content")
		}
	}
	machineFile(t, f, map[string]string{"op": "preview", "path": path.Join(root, "unsafe.html")}, 422)
	p := path.Join(root, "large.png")
	file, err := os.Create(localSFTPFixturePath(p))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(previewLimit + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	machineFile(t, f, map[string]string{"op": "preview", "path": p}, 413)
}

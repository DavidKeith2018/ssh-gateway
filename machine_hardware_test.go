package gateway

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"strings"
	"testing"
)

func TestHardwareParser(t *testing.T) {
	in := "Linux\n__GW_HOST__\nnode-1\n__GW_ARCH__\naarch64\n__GW_KERNEL__\n6.8\n__GW_OS__\nPRETTY_NAME=\"示例 Linux\"\n__GW_CPU__\nprocessor : 0\nProcessor : ARMv8\nprocessor : 1\n__GW_MEMORY__\nMemTotal: 1048576 kB\n__GW_PRODUCT__\nVirtual Machine\n__GW_END__\n"
	info := parseHardware(in)
	if info.OS != "示例 Linux" || info.CPUModel != "ARMv8" || info.LogicalCPUs != 2 || info.MemoryTotal != 1<<30 || info.Architecture != "aarch64" || info.Hostname != "node-1" || info.Product != "Virtual Machine" {
		t.Fatalf("硬件解析错误：%+v", info)
	}
	missing := parseHardware("Linux\n__GW_CPU__\nprocessor : not-a-number\n__GW_MEMORY__\nMemTotal: invalid\n")
	if missing.LogicalCPUs != 0 || missing.MemoryTotal != 0 || missing.CPUModel != "" {
		t.Fatal("缺失字段不能伪造为有效信息")
	}
	var output hardwareOutput
	if _, err := output.Write([]byte(strings.Repeat("a", (2<<20)+1))); err == nil {
		t.Fatal("未限制硬件输出大小")
	}
}
func TestHardwareEndpointAuthorization(t *testing.T) {
	f := newWebFixture(t)
	if r := f.request(t, "GET", "/api/targets/test/hardware", nil, nil); r.StatusCode != 401 {
		t.Fatal("硬件接口绕过登录")
	}
	f.login(t)
	r := f.request(t, "GET", "/api/targets/test/hardware", nil, nil)
	var info hardwareInfo
	json.NewDecoder(r.Body).Decode(&info)
	if r.StatusCode != 200 || info.CPUModel != "Fixture CPU" || info.LogicalCPUs != 2 || info.MemoryTotal != 16<<30 || info.DiskTotal != 100<<30 {
		t.Fatalf("硬件接口错误：%d %+v", r.StatusCode, info)
	}
	in := f.input
	in.AllowedSources = []string{"192.0.2.1"}
	if _, err := f.store.Put(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if r := f.request(t, "GET", "/api/targets/test/hardware", nil, nil); r.StatusCode != 403 {
		t.Fatal("硬件接口绕过来源规则")
	}
}

func TestDirectoryLinksRemainBrowsable(t *testing.T) {
	f := newWebFixture(t)
	f.login(t)
	root := machineFile(t, f, map[string]string{"op": "list"}, 200)["path"].(string)
	dir := path.Join(root, "folder")
	if err := os.Mkdir(localSFTPFixturePath(dir), 0700); err != nil {
		t.Fatal(err)
	}
	link := path.Join(root, "linked-folder")
	if err := os.Symlink(localSFTPFixturePath(dir), localSFTPFixturePath(link)); err != nil {
		t.Skip(err)
	}
	response := f.request(t, "POST", "/api/targets/test/files", map[string]string{"op": "list", "path": root}, nil)
	var listing struct {
		Entries []fileEntry `json:"entries"`
	}
	json.NewDecoder(response.Body).Decode(&listing)
	found := false
	for _, entry := range listing.Entries {
		if entry.Path == link {
			found = entry.Kind == "directory" && entry.Link
		}
	}
	if !found {
		t.Fatal("目录符号链接未识别为可展开目录")
	}
	machineFile(t, f, map[string]string{"op": "list", "path": link}, 200)
	machineFile(t, f, map[string]string{"op": "delete", "path": link}, 200)
	if _, err := os.Stat(localSFTPFixturePath(dir)); err != nil {
		t.Fatal("删除符号链接误删了目标目录")
	}
}

func TestHardwareDiskCapacity(t *testing.T) {
	for _, tc := range []struct {
		row  string
		want uint64
	}{
		{"/dev/root 104857600 42 99 42% /", 100 << 30},
		{"/dev/root invalid 42 99 42% /", 0},
		{"/dev/root 18446744073709551615 42 99 42% /", 0},
		{"/dev/root 104857600 42 99 42% /data", 0},
		{"", 0},
	} {
		if got := parseHardware("Linux\n__GW_DISK__\nFilesystem 1024-blocks Used Available Capacity Mounted on\n" + tc.row).DiskTotal; got != tc.want {
			t.Fatalf("容量解析错误: %q: %d != %d", tc.row, got, tc.want)
		}
	}
}

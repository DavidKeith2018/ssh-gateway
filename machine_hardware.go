package gateway

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 固定的只读命令；os-release 只当作文本解析，不作为 shell 脚本加载。
const hardwareCommand = `uname -s; printf '\n__GW_HOST__\n'; hostname; printf '\n__GW_ARCH__\n'; uname -m; printf '\n__GW_KERNEL__\n'; uname -r; printf '\n__GW_OS__\n'; cat /etc/os-release 2>/dev/null; printf '\n__GW_CPU__\n'; cat /proc/cpuinfo 2>/dev/null; printf '\n__GW_MEMORY__\n'; cat /proc/meminfo 2>/dev/null; printf '\n__GW_DISK__\n'; LC_ALL=C df -Pk / 2>/dev/null; printf '\n__GW_PRODUCT__\n'; cat /sys/class/dmi/id/product_name 2>/dev/null; printf '\n__GW_END__\n'`

type hardwareInfo struct {
	Hostname     string `json:"hostname"`
	OS           string `json:"os"`
	Kernel       string `json:"kernel"`
	Architecture string `json:"architecture"`
	CPUModel     string `json:"cpu_model"`
	LogicalCPUs  int    `json:"logical_cpus"`
	MemoryTotal  uint64 `json:"memory_total"`
	DiskTotal    uint64 `json:"disk_total"`
	Product      string `json:"product"`
}

func parseHardware(data string) hardwareInfo {
	out := hardwareInfo{}
	section := "system"
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "__GW_") && strings.HasSuffix(line, "__") {
			section = line
			continue
		}
		if line == "" {
			continue
		}
		switch section {
		case "system":
			if out.OS == "" {
				out.OS = line
			}
		case "__GW_HOST__":
			out.Hostname = line
		case "__GW_ARCH__":
			out.Architecture = line
		case "__GW_KERNEL__":
			out.Kernel = line
		case "__GW_PRODUCT__":
			out.Product = line
		case "__GW_OS__":
			if value, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
				if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
					value = value[1 : len(value)-1]
				} else if decoded, err := strconv.Unquote(value); err == nil {
					value = decoded
				}
				out.OS = value
			}
		case "__GW_CPU__":
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key == "processor" {
				if number, err := strconv.Atoi(value); err == nil && number >= 0 {
					out.LogicalCPUs++
				}
			}
			if out.CPUModel == "" && (key == "model name" || key == "Processor" || key == "Hardware") {
				out.CPUModel = value
			}
		case "__GW_DISK__":
			fields := strings.Fields(line)
			if len(fields) >= 6 && fields[len(fields)-1] == "/" {
				n, err := strconv.ParseUint(fields[len(fields)-5], 10, 64)
				if err == nil && n > 0 && n <= ^uint64(0)/1024 {
					out.DiskTotal = n * 1024
				}
			}
		case "__GW_MEMORY__":
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "MemTotal:" {
				n, err := strconv.ParseUint(fields[1], 10, 64)
				if err == nil && n < 1<<54 {
					out.MemoryTotal = n * 1024
				}
			}
		}
	}
	return out
}

type hardwareOutput struct{ buffer bytes.Buffer }

func (b *hardwareOutput) String() string { return b.buffer.String() }

func (b *hardwareOutput) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > 2<<20 {
		return 0, fmt.Errorf("目标硬件信息响应过大")
	}
	return b.buffer.Write(p)
}
func (web *Web) hardware(w http.ResponseWriter, r *http.Request) {
	client, _, done, err := web.machineClient(r, 8*time.Second)
	if err != nil {
		machineFailure(w, err)
		return
	}
	defer done()
	session, err := client.NewSession()
	if err != nil {
		machineFailure(w, err)
		return
	}
	defer session.Close()
	var out hardwareOutput
	session.Stdout = &out
	if err := session.Run(hardwareCommand); err != nil {
		machineFailure(w, fmt.Errorf("无法读取目标硬件信息：%w", err))
		return
	}
	jsonResponse(w, 200, parseHardware(out.String()))
}

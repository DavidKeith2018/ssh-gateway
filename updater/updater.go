// Package updater 检查 GitHub 正式版本，供页面提示用户手动升级。
package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var Version = "0.1.0"
var Repository = "" // 发布时通过 -ldflags 注入 owner/repo。

var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type Release struct {
	Tag        string `json:"tag_name"`
	Notes      string `json:"body"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}
type Info struct {
	Configured     bool   `json:"configured"`
	CurrentVersion string `json:"current_version"`
	Version        string `json:"version"`
	Available      bool   `json:"available"`
	URL            string `json:"url"`
	Notes          string `json:"notes"`
}
type Client struct {
	Repository string
	Version    string
	mu         sync.Mutex
	checked    time.Time
	info       Info
	err        error
	HTTP       *http.Client
}

func New(repository, version string) *Client {
	return &Client{Repository: repository, Version: version, HTTP: &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("更新检查重定向次数过多")
			}
			if !trustedURL(req.URL) {
				return fmt.Errorf("更新检查重定向到不受信任的地址")
			}
			return nil
		},
	}}
}
func parseVersion(value string) ([3]uint64, error) {
	var result [3]uint64
	match := versionPattern.FindStringSubmatch(value)
	if match == nil {
		return result, fmt.Errorf("不支持的正式版本号：%s", value)
	}
	for i := range result {
		n, err := strconv.ParseUint(match[i+1], 10, 64)
		if err != nil {
			return result, fmt.Errorf("版本号超出范围")
		}
		result[i] = n
	}
	return result, nil
}
func newer(candidate, current string) (bool, error) {
	a, err := parseVersion(candidate)
	if err != nil {
		return false, err
	}
	b, err := parseVersion(current)
	if err != nil {
		return false, err
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i], nil
		}
	}
	return false, nil
}
func trustedURL(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	switch u.Hostname() {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	}
	return false
}
func (c *Client) get(ctx context.Context, address string) (*http.Response, error) {
	u, err := url.Parse(address)
	if err != nil || !trustedURL(u) {
		return nil, fmt.Errorf("更新地址必须来自 GitHub HTTPS")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ssh-gateway/"+c.Version)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接 GitHub 失败：%w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		if resp.StatusCode == 404 {
			return nil, fmt.Errorf("尚无可用的公开正式版本，或发布仓库不存在")
		}
		if resp.StatusCode == 403 || resp.StatusCode == 429 {
			return nil, fmt.Errorf("GitHub 暂时限制请求，请稍后重试")
		}
		return nil, fmt.Errorf("GitHub 返回 HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

// Check 缓存一分钟内的检查结果（包括失败），避免重复访问 GitHub。
func (c *Client) Check(ctx context.Context) (Info, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.checked.IsZero() && time.Since(c.checked) < time.Minute {
		return c.info, c.err
	}
	c.info, c.err = c.check(ctx)
	c.checked = time.Now()
	return c.info, c.err
}
func (c *Client) check(ctx context.Context) (Info, error) {
	info := Info{CurrentVersion: c.Version, Configured: c.Repository != ""}
	if !info.Configured {
		return info, nil
	}
	if !repositoryPattern.MatchString(c.Repository) {
		return info, fmt.Errorf("发布仓库格式应为 owner/repo")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	resp, err := c.get(ctx, "https://api.github.com/repos/"+c.Repository+"/releases/latest")
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil {
		return info, err
	}
	if len(data) > 2<<20 {
		return info, fmt.Errorf("版本信息过大")
	}
	var release Release
	if err := json.Unmarshal(data, &release); err != nil {
		return info, fmt.Errorf("版本信息格式错误：%w", err)
	}
	if release.Draft || release.Prerelease {
		return info, fmt.Errorf("更新源不是正式发布")
	}
	available, err := newer(release.Tag, c.Version)
	if err != nil {
		return info, err
	}
	info.Version = strings.TrimPrefix(release.Tag, "v")
	info.Notes = release.Notes
	info.URL = "https://github.com/" + c.Repository + "/releases/tag/" + release.Tag
	info.Available = available
	return info, nil
}

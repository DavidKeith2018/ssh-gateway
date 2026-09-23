package updater

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(t *testing.T, release Release, bodies map[string]string) *Client {
	t.Helper()
	c := New("owner/repo", "0.2.0")
	c.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
		data := ""
		status := 200
		if r.URL.Path == "/repos/owner/repo/releases/latest" {
			b, _ := json.Marshal(release)
			data = string(b)
		} else {
			var ok bool
			data, ok = bodies[filepath.Base(r.URL.Path)]
			if !ok {
				status = 404
			}
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(data)), Header: make(http.Header), Request: r}, nil
	})
	return c
}
func validRelease() Release { return Release{Tag: "v0.3.0", Notes: "更新说明"} }
func TestVersionComparison(t *testing.T) {
	for _, tc := range []struct {
		candidate, current string
		want, invalid      bool
	}{
		{"v0.10.0", "0.9.0", true, false}, {"1.0.0", "0.99.99", true, false},
		{"v0.2.0", "0.2.0", false, false}, {"0.1.9", "0.2.0", false, false},
		{"v0.3.0-rc.1", "0.2.0", false, true}, {"v00.3.0", "0.2.0", false, true},
		{"1.2", "0.2.0", false, true}, {"1.2.3", "dev", false, true},
		{"18446744073709551616.0.0", "0.2.0", false, true},
	} {
		t.Run(tc.candidate+"/"+tc.current, func(t *testing.T) {
			got, err := newer(tc.candidate, tc.current)
			if (err != nil) != tc.invalid || got != tc.want {
				t.Fatalf("结果 %v, %v", got, err)
			}
		})
	}
}
func TestCheckRelease(t *testing.T) {
	for _, tc := range []struct {
		name            string
		release         Release
		available, fail bool
	}{
		{"只有发布说明也提示新版本", validRelease(), true, false},
		{"草稿", Release{Tag: "v0.3.0", Draft: true}, false, true},
		{"预发布", Release{Tag: "v0.3.0", Prerelease: true}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info, err := fixture(t, tc.release, nil).Check(context.Background())
			if (err != nil) != tc.fail || info.Available != tc.available {
				t.Fatalf("%+v %v", info, err)
			}
			if tc.available && info.URL != "https://github.com/owner/repo/releases/tag/v0.3.0" {
				t.Fatal(info.URL)
			}
		})
	}
}
func TestCheckCachesSuccessAndFailure(t *testing.T) {
	for _, status := range []int{200, 429} {
		c := New("owner/repo", "0.2.0")
		calls := 0
		c.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v0.3.0"}`))}, nil
		})
		for range 3 {
			c.Check(context.Background())
		}
		if calls != 1 {
			t.Fatalf("一分钟内发起 %d 次请求", calls)
		}
	}
}
func TestUnconfiguredAndNoDowngrade(t *testing.T) {
	c := New("", "0.2.0")
	info, err := c.Check(context.Background())
	if err != nil || info.Configured || info.Available {
		t.Fatalf("%+v %v", info, err)
	}
	for _, tag := range []string{"v0.1.0", "v0.2.0"} {
		r := validRelease()
		r.Tag = tag
		info, err := fixture(t, r, nil).Check(context.Background())
		if err != nil || info.Available {
			t.Fatalf("不应降级 %+v %v", info, err)
		}
	}
}
func TestNetworkErrorsAndRedirectPolicy(t *testing.T) {
	for _, status := range []int{404, 403, 429, 500} {
		c := New("owner/repo", "0.2.0")
		c.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("error"))}, nil
		})
		if _, err := c.Check(context.Background()); err == nil {
			t.Fatalf("忽略了 HTTP %d", status)
		}
	}
	c := New("owner/repo", "0.2.0")
	for _, address := range []string{"http://github.com/file", "https://evil.example/file", "https://github.com.evil.example/file", "https://user@github.com/file", "https://github.com:8443/file"} {
		req, _ := http.NewRequest("GET", address, nil)
		if c.HTTP.CheckRedirect(req, nil) == nil {
			t.Fatalf("接受不可信重定向 %s", address)
		}
	}
	req, _ := http.NewRequest("GET", "https://release-assets.githubusercontent.com/file", nil)
	if err := c.HTTP.CheckRedirect(req, nil); err != nil {
		t.Fatal(err)
	}
}

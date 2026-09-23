package localization

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestErrorMetadata(t *testing.T) {
	for _, source := range []string{"用户名或密码不正确", "凭证已锁定，请先输入主密码解锁", "文件超过 2 MiB，请下载查看", "已连接"} {
		if info := Describe(source); info.Key == "" {
			t.Fatalf("缺少消息标识：%s", source)
		}
	}
	// 动态用户名和百分号保持原值；嵌套应用错误单独带消息标识。
	info := Describe("请填写目标账号 删除{0}% 的密码")
	if len(info.Params) != 1 || info.Params[0] != "删除{0}%" {
		t.Fatalf("用户名被修改：%+v", info)
	}
	info = Describe("保存失败：SSH 连接失败：目标 SSH 主机指纹不匹配")
	if len(info.Params) != 1 {
		t.Fatalf("嵌套错误缺少参数：%+v", info)
	}
	if nested, ok := info.Params[0].(Message); !ok || nested.Key == "" {
		t.Fatalf("嵌套错误缺少标识：%+v", info)
	}
	if Describe("remote diagnostic: 删除").Key != "" {
		t.Fatal("第三方原始诊断不应按片段翻译")
	}
	err := fmt.Errorf("连接测试失败：%s", "目标 SSH 主机指纹不匹配")
	var wire map[string]any
	if json.Unmarshal(MarshalError(err), &wire) != nil || wire["error"] != err.Error() || wire["message_key"] == "" {
		t.Fatalf("原始错误不兼容：%v", wire)
	}
}

func TestCatalogAndNativeLabels(t *testing.T) {
	for key, item := range catalog {
		if item.English == "" || item.Chinese == "" {
			t.Fatalf("词条不完整：%s", key)
		}
		for _, placeholder := range regexpParams(item.Chinese) {
			if !strings.Contains(item.English, placeholder) {
				t.Fatalf("英文参数缺失：%s %s", key, placeholder)
			}
		}
	}
	if Text("en", "显示主窗口") != "Show main window" || Text("zh-CN", "显示主窗口") != "显示主窗口" {
		t.Fatal("托盘翻译错误")
	}
	if Text("en", "保存下载文件") != "Save downloaded file" {
		t.Fatal("文件框翻译错误")
	}
}

func regexpParams(value string) []string {
	var result []string
	for n := 0; n < 20; n++ {
		p := fmt.Sprintf("{%d}", n)
		if strings.Contains(value, p) {
			result = append(result, p)
		}
	}
	return result
}

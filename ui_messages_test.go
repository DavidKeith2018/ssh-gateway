package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestLocalizedWireCompatibility(t *testing.T) {
	w := httptest.NewRecorder()
	apiError(w, 401, "用户名或密码不正确")
	var body map[string]any
	if json.Unmarshal(w.Body.Bytes(), &body) != nil || w.Code != 401 || body["error"] != "用户名或密码不正确" || body["message_key"] == "" {
		t.Fatalf("错误接口不兼容：%s", w.Body.String())
	}
	for _, event := range []TerminalEvent{
		{ID: "t", Type: "ready", Message: "已连接"},
		{ID: "t", Type: "connection", Message: `{"user":"删除"}`},
		{ID: "t", Type: "output", Data: "原始终端输出"},
	} {
		wire, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		var result TerminalEvent
		if err := json.Unmarshal(wire, &result); err != nil || result != event {
			t.Fatalf("终端数据被修改：%s", wire)
		}
		var metadata map[string]any
		json.Unmarshal(wire, &metadata)
		_, hasKey := metadata["message_key"]
		if hasKey != (event.Type == "ready") {
			t.Fatalf("状态元数据错误：%s", wire)
		}
	}
	request := httptest.NewRequest("GET", "/desktop/window?lang=en", nil)
	if windowLanguageQuery(request) != "&lang=en" {
		t.Fatal("独立窗口未继承英文")
	}
	request = httptest.NewRequest("GET", "/desktop/window", nil)
	if windowLanguageQuery(request) != "" {
		t.Fatal("无语言参数的旧链接应保持兼容")
	}
	info := LocalizeDesktopInfo(DesktopInfo{Ready: true, Error: "服务未初始化"})
	if !info.Ready || info.Error != "服务未初始化" || info.MessageKey == "" {
		t.Fatalf("桌面状态不兼容：%+v", info)
	}
	view := MappingView{Status: "error", Error: "映射不存在", LastError: "SSH 连接已断开，请重试", Warning: "原始第三方诊断"}
	wire, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	json.Unmarshal(wire, &result)
	if result["error"] != view.Error || result["last_error"] != view.LastError || result["warning"] != view.Warning {
		t.Fatalf("映射原始字段被修改：%s", wire)
	}
	if result["error_i18n"].(map[string]any)["message_key"] == "" || result["last_error_i18n"].(map[string]any)["message_key"] == "" {
		t.Fatalf("映射缺少翻译元数据：%s", wire)
	}
}

package gateway

import (
	"encoding/json"
	"ssh-gateway/localization"
)

// 元数据采用增量字段，调用方仍可读取原始诊断。
func LocalizeDesktopInfo(info DesktopInfo) DesktopInfo {
	message := localization.Describe(info.Error)
	info.MessageKey, info.MessageParams = message.Key, message.Params
	return info
}

func (event TerminalEvent) MarshalJSON() ([]byte, error) {
	type plain TerminalEvent
	info := localization.Message{}
	if event.Type == "error" || event.Type == "closed" || event.Type == "ready" {
		info = localization.Describe(event.Message)
	}
	return json.Marshal(struct {
		plain
		Key    string `json:"message_key,omitempty"`
		Params []any  `json:"message_params,omitempty"`
	}{plain(event), info.Key, info.Params})
}

func (view MappingView) MarshalJSON() ([]byte, error) {
	type plain MappingView
	return json.Marshal(struct {
		plain
		ErrorMessage     localization.Message `json:"error_i18n"`
		LastErrorMessage localization.Message `json:"last_error_i18n"`
		WarningMessage   localization.Message `json:"warning_i18n"`
	}{plain(view), localization.Describe(view.Error), localization.Describe(view.LastError), localization.Describe(view.Warning)})
}

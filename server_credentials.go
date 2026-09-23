package gateway

import "net/http"

// 原始服务器凭证只通过管理员显式读取接口返回。
func (web *Web) serverCredentials(w http.ResponseWriter, r *http.Request) {
	if web.store.MasterPasswordStatus().Locked {
		apiError(w, 423, "请先解锁凭证")
		return
	}
	target, err := web.store.get(r.Context(), "id", r.PathValue("id"))
	if err != nil {
		apiError(w, 404, "目标不存在")
		return
	}
	logins := target.Logins
	if len(logins) == 0 {
		logins = []TargetLogin{{ID: "default", User: target.User, AuthType: target.AuthType}}
	}
	for _, login := range logins {
		if login.ID != r.PathValue("login") {
			continue
		}
		secrets, err := web.store.savedSecrets(target)
		if err != nil {
			apiError(w, 500, "读取服务器凭证失败")
			return
		}
		secret, ok := secrets.Logins[login.ID]
		if !ok {
			apiError(w, 404, "服务器凭证不存在")
			return
		}
		web.store.logEvent(target.ID, sourceIP(r), "管理员读取服务器原始凭证："+login.User)
		jsonResponse(w, 200, struct {
			Target     Target      `json:"target"`
			Login      TargetLogin `json:"login"`
			Credential loginSecret `json:"credential"`
		}{target.Target, login, secret})
		return
	}
	apiError(w, 404, "服务器账号不存在")
}

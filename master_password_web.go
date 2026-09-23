package gateway

import (
	"net"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func (web *Web) masterPasswordRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/security", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, web.store.MasterPasswordStatus())
	})
	mux.HandleFunc("POST /api/unlock", web.unlockCredentials)
	mux.HandleFunc("POST /api/security/master-password", web.administrator(web.changeMasterPassword))
}

// 主密码只允许通过 HTTPS 或回环连接传输；桌面桥接使用回环请求。
func (web *Web) allowMasterPasswordRequest(w http.ResponseWriter, r *http.Request) bool {
	ip := net.ParseIP(sourceIP(r))
	if !web.secureRequest(r) && (ip == nil || !ip.IsLoopback()) {
		apiError(w, 403, "主密码操作需要 HTTPS 或本机回环连接")
		return false
	}
	// 独立计数，和登录限流区分。单实例最多同时执行一次内存密集的 KDF。
	key, now := "master:"+sourceIP(r), time.Now()
	web.mu.Lock()
	for k, a := range web.attempts {
		if now.After(a.until) {
			delete(web.attempts, k)
		}
	}
	attempt := web.attempts[key]
	if attempt.count >= 10 || len(web.attempts) >= 1024 {
		web.mu.Unlock()
		apiError(w, 429, "主密码尝试过多，请一分钟后再试")
		return false
	}
	if attempt.count == 0 {
		attempt.until = now.Add(time.Minute)
	}
	attempt.count++
	web.attempts[key] = attempt
	web.mu.Unlock()
	select {
	case web.masterSlots <- struct{}{}:
		return true
	default:
		apiError(w, 429, "正在处理主密码，请稍后重试")
		return false
	}
}

func (web *Web) unlockCredentials(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	if !web.allowMasterPasswordRequest(w, r) {
		return
	}
	defer func() { <-web.masterSlots }()
	if err := web.store.UnlockMasterPassword(in.Password); err != nil {
		apiError(w, 401, err.Error())
		return
	}
	jsonResponse(w, 200, web.store.MasterPasswordStatus())
}

func (web *Web) changeMasterPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Action        string `json:"action"`
		AdminPassword string `json:"admin_password"`
		Current       string `json:"current"`
		Password      string `json:"password"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	if in.Action != "enable" && in.Action != "change" && in.Action != "disable" {
		apiError(w, 400, "主密码操作无效")
		return
	}
	if in.Action == "disable" && in.Password != "" {
		apiError(w, 400, "关闭保护时不能同时设置新主密码")
		return
	}
	if in.Action != "disable" {
		if err := validateMasterPassword(in.Password); err != nil {
			apiError(w, 400, err.Error())
			return
		}
	}
	if !web.allowMasterPasswordRequest(w, r) {
		return
	}
	defer func() { <-web.masterSlots }()
	hash, err := web.store.adminHash(r.Context())
	if err != nil || bcrypt.CompareHashAndPassword(hash, []byte(in.AdminPassword)) != nil {
		apiError(w, 403, "管理员密码不正确")
		return
	}
	enabled := web.store.MasterPasswordStatus().Enabled
	if (in.Action == "enable") == enabled {
		apiError(w, 409, "主密码状态已改变，请重新打开安全设置")
		return
	}
	if err := web.store.ChangeMasterPassword(in.Current, in.Password); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	web.store.logEvent("", sourceIP(r), "主密码保护设置已更新")
	jsonResponse(w, 200, web.store.MasterPasswordStatus())
}

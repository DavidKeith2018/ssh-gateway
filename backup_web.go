package gateway

import (
	"golang.org/x/crypto/bcrypt"
	"net/http"
)

func (web *Web) exportBackup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password      string `json:"password"`
		AdminPassword string `json:"admin_password"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		apiError(w, 400, err.Error())
		return
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
	data, err := web.store.ExportBackup(r.Context(), in.Password)
	if err != nil {
		apiError(w, 400, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	web.store.logEvent("", sourceIP(r), "已导出加密备份")
	jsonResponse(w, 200, map[string]any{"filename": "ssh-gateway-backup.sgb", "data": data})
}

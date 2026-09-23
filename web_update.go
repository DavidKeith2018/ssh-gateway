package gateway

import (
	"net/http"
	"ssh-gateway/updater"
)

func (web *Web) programVersion(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, map[string]string{"version": updater.Version})
}
func (web *Web) programUpdate(w http.ResponseWriter, r *http.Request) {
	info, err := web.updateClient.Check(r.Context())
	if err != nil {
		apiError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, info)
}

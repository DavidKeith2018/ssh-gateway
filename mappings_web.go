package gateway

import (
	"net/http"
)

func (web *Web) mappingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/mappings/summary", web.administrator(web.mappingSummary))
	mux.HandleFunc("GET /api/mappings", web.administrator(web.mappingPage))
	mux.HandleFunc("POST /api/mappings", web.administrator(web.saveMapping))
	mux.HandleFunc("PUT /api/mappings/{mapping}", web.administrator(web.saveMapping))
	mux.HandleFunc("DELETE /api/mappings/{mapping}", web.administrator(web.removeMapping))
	mux.HandleFunc("POST /api/mappings/{mapping}/start", web.administrator(web.startMapping))
	mux.HandleFunc("POST /api/mappings/{mapping}/stop", web.administrator(web.stopMapping))
}
func (web *Web) saveMapping(w http.ResponseWriter, r *http.Request) {
	var in Mapping
	if err := decodeJSON(w, r, &in); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	create := r.Method == "POST"
	if !create && in.ID != r.PathValue("mapping") {
		apiError(w, 400, "映射 ID 不匹配")
		return
	}
	saved, err := web.mappings.Save(r.Context(), in, sourceIP(r), create)
	if err != nil {
		machineFailure(w, err)
		return
	}
	web.store.logEvent(saved.TargetID, sourceIP(r), "端口映射配置已保存："+saved.Name)
	jsonResponse(w, 200, saved)
}
func (web *Web) removeMapping(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("mapping")
	if err := web.mappings.Delete(r.Context(), id); err != nil {
		machineFailure(w, err)
		return
	}
	web.store.logEvent("", sourceIP(r), "端口映射已删除："+id)
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func (web *Web) startMapping(w http.ResponseWriter, r *http.Request) {
	if err := web.mappings.Start(r.Context(), r.PathValue("mapping"), sourceIP(r)); err != nil {
		machineFailure(w, err)
		return
	}
	jsonResponse(w, 202, map[string]bool{"ok": true})
}
func (web *Web) stopMapping(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("mapping")
	item, err := web.store.mapping(r.Context(), id)
	if err != nil {
		apiError(w, 404, "映射不存在")
		return
	}
	web.mappings.Stop(id)
	web.store.logEvent(item.TargetID, sourceIP(r), "端口映射已停止："+item.Name)
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

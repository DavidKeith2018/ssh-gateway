package gateway

import (
	"fmt"
	"net/http"
	"strings"
)

type mappingCount struct {
	Total   int `json:"total"`
	Running int `json:"running"`
}
type mappingSummary struct {
	Total    int                     `json:"total"`
	Running  int                     `json:"running"`
	Stopped  int                     `json:"stopped"`
	Error    int                     `json:"error"`
	ByTarget map[string]mappingCount `json:"by_target"`
}

func summarizeMappings(items []MappingView) mappingSummary {
	s := mappingSummary{Total: len(items), ByTarget: map[string]mappingCount{}}
	for _, v := range items {
		c := s.ByTarget[v.TargetID]
		c.Total++
		switch v.Status {
		case "running":
			s.Running++
		case "stopped":
			s.Stopped++
		case "error":
			s.Error++
		}
		if v.Status == "running" {
			c.Running++
		}
		s.ByTarget[v.TargetID] = c
	}
	return s
}
func (web *Web) mappingSummary(w http.ResponseWriter, r *http.Request) {
	items, err := web.mappings.Views(r.Context())
	if err != nil {
		machineFailure(w, err)
		return
	}
	jsonResponse(w, 200, summarizeMappings(items))
}
func (web *Web) mappingPage(w http.ResponseWriter, r *http.Request) {
	p, ok := readPage(w, r)
	if !ok {
		return
	}
	items, err := web.mappings.Views(r.Context())
	if err != nil {
		machineFailure(w, err)
		return
	}
	summary := summarizeMappings(items)
	targets, err := web.store.List(r.Context())
	if err != nil {
		machineFailure(w, err)
		return
	}
	names := map[string]string{}
	for _, t := range targets {
		names[t.ID] = t.Name + " " + t.Host
	}
	q := r.URL.Query()
	search := strings.ToLower(strings.TrimSpace(q.Get("q")))
	filtered := make([]MappingView, 0)
	for _, v := range items {
		if q.Get("target_id") != "" && v.TargetID != q.Get("target_id") || q.Get("direction") != "" && v.Direction != q.Get("direction") || q.Get("scope") != "" && v.Scope != q.Get("scope") || q.Get("status") != "" && v.Status != q.Get("status") {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(fmt.Sprintf("%s %s %s %d %d %s", v.Name, names[v.TargetID], v.ServiceHost, v.ServicePort, v.ListenPort, strings.Join(v.AccessAddresses, " "))), search) {
			continue
		}
		filtered = append(filtered, v)
	}
	p.clamp(len(filtered))
	start := (p.Page - 1) * p.PageSize
	end := min(start+p.PageSize, len(filtered))
	jsonResponse(w, 200, struct {
		pageResult[MappingView]
		Summary        mappingSummary `json:"summary"`
		LocalDesktop   bool           `json:"local_desktop"`
		LocalAddresses []string       `json:"local_addresses"`
	}{pageResult[MappingView]{filtered[start:end], len(filtered), p}, summary, web.localDesktop, localMappingIPs()})
}

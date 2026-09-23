package main

import "ssh-gateway/updater"

type updateState struct{ client *updater.Client }

func (a *App) CheckUpdate() (updater.Info, error) {
	a.mu.Lock()
	if a.updates.client == nil {
		a.updates.client = updater.New(updater.Repository, updater.Version)
	}
	client := a.updates.client
	a.mu.Unlock()
	return client.Check(a.ctx)
}

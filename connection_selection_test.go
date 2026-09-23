package gateway

import (
	"context"
	"fmt"
	"testing"
)

func TestConnectionSelectionUsesSavedAccount(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	in := multiLoginInput(t)
	if _, err := f.store.PutTarget(ctx, in); err != nil {
		t.Fatal(err)
	}
	for selection, user := range map[string]string{"ubuntu-relay": "ubuntu", "root-relay": "root", "server:ubuntu-login": "ubuntu", "server:root-login": "root"} {
		t.Run(selection, func(t *testing.T) {
			selected, err := f.store.connectionRecord(ctx, in.ID, selection)
			if err != nil {
				t.Fatal(err)
			}
			client, err := f.store.dial(ctx, selected)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			session, err := client.NewSession()
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			output, err := session.Output("whoami")
			if err != nil || string(output) != user+"\n" {
				t.Fatalf("账号不匹配：%q %v", output, err)
			}
		})
	}
	for _, selection := range []string{"missing", "server:missing", "server:", "root-login"} {
		if _, err := f.store.connectionRecord(ctx, in.ID, selection); err == nil {
			t.Fatalf("无效账号被接受：%s", selection)
		}
	}
	target, _ := f.store.Get(ctx, in.ID)
	update := PutInput{Target: target}
	for _, relay := range target.Relays {
		update.RelayInputs = append(update.RelayInputs, TargetRelayInput{TargetRelay: relay})
	}
	update.RelayInputs[1].Enabled = false
	if _, err := f.store.PutTarget(ctx, update); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.connectionRecord(ctx, in.ID, "root-relay"); err == nil {
		t.Fatal("禁用中转仍可连接")
	}
}

func TestTerminalRejectsInvalidSelectedAccount(t *testing.T) {
	f := newWebFixture(t)
	f.login(t)
	for _, selection := range []string{"missing", "server:missing"} {
		requireStatus(t, f, "GET", fmt.Sprintf("/api/targets/test/terminal?connection=%s", selection), nil, 403)
	}
}

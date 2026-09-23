package main

import (
	"context"
	"errors"
	"testing"
)

func TestOverwriteRequiresExplicitConfirmation(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		var decisions transferDecisions
		err := decisions.wait(context.Background(), "upload", func() {
			decisions.reply("other", true)
			decisions.reply("upload", confirmed)
			decisions.reply("upload", !confirmed)
		})
		if (err == nil) != confirmed {
			t.Fatalf("确认=%v，返回=%v", confirmed, err)
		}
		if _, ok := decisions.pending.Load("upload"); ok {
			t.Fatal("确认结束后未清理")
		}
	}
}

func TestOverwriteCancelledWithTransfer(t *testing.T) {
	var decisions transferDecisions
	ctx, cancel := context.WithCancel(context.Background())
	err := decisions.wait(ctx, "upload", cancel)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	decisions.reply("upload", true)
	if _, ok := decisions.pending.Load("upload"); ok {
		t.Fatal("取消后未清理")
	}
}

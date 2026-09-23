package main

import (
	"context"
	"fmt"
	"sync"
)

type transferDecisions struct{ pending sync.Map }

func (d *transferDecisions) wait(ctx context.Context, id string, emit func()) error {
	answer := make(chan bool, 1)
	d.pending.Store(id, answer)
	defer d.pending.Delete(id)
	emit()
	select {
	case confirmed := <-answer:
		if err := ctx.Err(); err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("已取消上传")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *transferDecisions) reply(id string, confirmed bool) {
	if pending, ok := d.pending.LoadAndDelete(id); ok {
		pending.(chan bool) <- confirmed
	}
}

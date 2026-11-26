package singleton

import (
	"errors"
	"sync"
	"time"

	"github.com/naiba/nezha/model"
	pb "github.com/naiba/nezha/proto"
)

const (
	maxPendingTasks = 65536
	maxPendingPerKey = 64
	monitorReceiptTTL = 5 * time.Minute
	commandReceiptTTL = 24 * time.Hour
)

type taskReceiptKey struct { agent, task, kind uint64 }
type taskReceipt struct { expires time.Time }
type receiptLedger struct {
	mu sync.Mutex
	items map[taskReceiptKey][]*taskReceipt
	count int
	lastSweep time.Time
}
var taskReceipts = receiptLedger{items: make(map[taskReceiptKey][]*taskReceipt)}

func (l *receiptLedger) prune(key taskReceiptKey, now time.Time) {
	old := l.items[key]
	n := 0
	for _, r := range old {
		if now.Before(r.expires) { old[n] = r; n++ } else { l.count-- }
	}
	clear(old[n:])
	if n == 0 { delete(l.items,key) } else { l.items[key] = old[:n] }
}

func (l *receiptLedger) reserve(key taskReceiptKey, now time.Time, ttl time.Duration) *taskReceipt {
	l.mu.Lock(); defer l.mu.Unlock()
	if now.Sub(l.lastSweep) >= time.Minute {
		for k := range l.items { l.prune(k, now) }
		l.lastSweep = now
	}
	l.prune(key, now)
	if l.count >= maxPendingTasks || len(l.items[key]) >= maxPendingPerKey { return nil }
	r := &taskReceipt{expires: now.Add(ttl)}
	l.items[key] = append(l.items[key], r)
	l.count++
	return r
}

func (l *receiptLedger) cancel(key taskReceiptKey, token *taskReceipt) {
	l.mu.Lock(); defer l.mu.Unlock()
	q := l.items[key]
	for i, r := range q {
		if r == token {
			copy(q[i:], q[i+1:]); q[len(q)-1] = nil; q = q[:len(q)-1]
			l.count--
			if len(q)==0 { delete(l.items,key) } else { l.items[key]=q }
			return
		}
	}
}

func (l *receiptLedger) consume(key taskReceiptKey, now time.Time) bool {
	l.mu.Lock(); defer l.mu.Unlock()
	l.prune(key, now)
	q := l.items[key]
	if len(q)==0 { return false }
	q[0]=nil
	l.count--
	if len(q)==1 { delete(l.items,key) } else { l.items[key]=q[1:] }
	return true
}

// Reserve before Send, since a fast Agent may reply immediately. No wire changes:
// V0 agents echo task ID and type, not a unique dispatch nonce.
func SendTrackedTask(server *model.Server, task *pb.Task) error {
	if server == nil || server.TaskStream == nil || task == nil { return errors.New("agent unavailable") }
	key := taskReceiptKey{server.ID, task.GetId(), task.GetType()}
	ttl := monitorReceiptTTL
	if key.kind == model.TaskTypeCommand { ttl = commandReceiptTTL }
	r := taskReceipts.reserve(key,time.Now(),ttl)
	if r == nil { return errors.New("pending task limit reached") }
	if err := server.TaskStream.Send(task); err != nil {
		taskReceipts.cancel(key,r)
		return err
	}
	return nil
}

func ConsumeTrackedTask(agent, task, kind uint64) bool {
	return taskReceipts.consume(taskReceiptKey{agent,task,kind}, time.Now())
}

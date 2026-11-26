package singleton

import (
	"testing"
	"time"
)

func TestReceiptOwnershipAndReplay(t *testing.T) {
	l := receiptLedger{items: make(map[taskReceiptKey][]*taskReceipt)}
	now := time.Now()
	key := taskReceiptKey{1,2,3}
	if l.reserve(key,now,time.Minute)==nil { t.Fatal("reservation failed") }
	for _, other := range []taskReceiptKey{{4,2,3},{1,4,3},{1,2,4}} {
		if l.consume(other,now) { t.Fatal("wrong identity accepted") }
	}
	if !l.consume(key,now) || l.consume(key,now) { t.Fatal("receipt must be single-use") }
	r := l.reserve(key,now,time.Minute)
	l.cancel(key,r)
	if l.consume(key,now) { t.Fatal("failed dispatch accepted") }
	l.reserve(key,now,time.Minute)
	if l.consume(key,now.Add(time.Minute)) { t.Fatal("expired receipt accepted") }
}

func TestReceiptLimit(t *testing.T) {
	l := receiptLedger{items: make(map[taskReceiptKey][]*taskReceipt)}
	now := time.Now()
	key := taskReceiptKey{1,2,3}
	for i:=0; i<maxPendingPerKey; i++ { if l.reserve(key,now,time.Minute)==nil { t.Fatal("premature limit") } }
	if l.reserve(key,now,time.Minute)!=nil { t.Fatal("limit bypass") }
	if !l.consume(key,now) || l.reserve(key,now,time.Minute)==nil { t.Fatal("capacity not reclaimed") }
}

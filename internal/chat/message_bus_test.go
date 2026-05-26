package chat

import (
	"sync"
	"testing"
	"time"
)

func TestNewMessageBus(t *testing.T) {
	mb := NewMessageBus(nil)
	if mb == nil {
		t.Fatal("MessageBus should not be nil")
	}
	if !mb.enabled {
		t.Fatal("MessageBus should be enabled by default")
	}
	if mb.timeout != 5*time.Second {
		t.Fatalf("Expected timeout 5s, got %v", mb.timeout)
	}
}

func TestSendAndAck(t *testing.T) {
	mb := NewMessageBus(nil)

	msgId := mb.Send("pm", "test content", "pm_message", PathPMToUser, "test_location", nil)
	if msgId == "" {
		t.Fatal("msgId should not be empty")
	}

	pending := mb.CheckPending()
	if len(pending) != 1 {
		t.Fatalf("Expected 1 pending message, got %d", len(pending))
	}

	ackSuccess := mb.Ack(msgId)
	if !ackSuccess {
		t.Fatal("ACK should succeed")
	}

	pending = mb.CheckPending()
	if len(pending) != 0 {
		t.Fatalf("Expected 0 pending messages after ACK, got %d", len(pending))
	}
}

func TestChecksumGeneration(t *testing.T) {
	mb := NewMessageBus(nil)

	content1 := "hello world"
	content2 := "hello world"
	content3 := "different content"

	checksum1 := mb.generateChecksum(content1)
	checksum2 := mb.generateChecksum(content2)
	checksum3 := mb.generateChecksum(content3)

	if checksum1 != checksum2 {
		t.Fatal("Same content should produce same checksum")
	}
	if checksum1 == checksum3 {
		t.Fatal("Different content should produce different checksum")
	}
}

func TestMsgIdUniqueness(t *testing.T) {
	mb := NewMessageBus(nil)

	id1 := mb.Send("pm", "content1", "pm_message", PathPMToUser, "loc1", nil)
	id2 := mb.Send("se", "content2", "se_stream", PathSEToUser, "loc2", nil)

	if id1 == id2 {
		t.Fatal("Different messages should have different IDs")
	}
}

func TestLostMessageDetection(t *testing.T) {
	mb := NewMessageBus(nil)
	mb.timeout = 1 * time.Second
	mb.checkInterval = 500 * time.Millisecond

	msgId := mb.Send("pm", "lost content", "pm_message", PathPMToUser, "test_loc", nil)

	time.Sleep(2 * time.Second)

	mb.CheckPending()

	lost := mb.GetLostMessages()
	if len(lost) == 0 {
		t.Fatalf("Expected at least 1 lost message, got %d", len(lost))
	}

	found := false
	for _, msg := range lost {
		if msg["msgId"] == msgId {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Sent message %s should be in lost list", msgId)
	}
}

func TestConcurrentAccess(t *testing.T) {
	mb := NewMessageBus(nil)

	var wg sync.WaitGroup
	var ids []string
	var mu sync.Mutex

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			msgId := mb.Send("pm", "concurrent test", "pm_message", PathPMToUser, "concurrent_loc", nil)
			mu.Lock()
			ids = append(ids, msgId)
			mu.Unlock()
			if idx%2 == 0 && msgId != "" {
				mb.Ack(msgId)
			}
		}(i)
	}

	wg.Wait()

	time.Sleep(200 * time.Millisecond)

	pending := mb.CheckPending()
	stats := mb.GetStats()

	pendingCount := len(pending)
	receivedCount := 0
	if rc, ok := stats["received"].(int); ok {
		receivedCount = rc
	}

	totalMsgs := pendingCount + receivedCount

	if totalMsgs > 100 {
		t.Fatalf("Total messages should be <= 100, got %d (pending=%d, received=%d)",
			totalMsgs, pendingCount, receivedCount)
	}
	if totalMsgs < 50 {
		t.Fatalf("Total messages should be >= 50 (至少一半应该被追踪), got %d (pending=%d, received=%d)",
			totalMsgs, pendingCount, receivedCount)
	}
}

func TestSetEnabled(t *testing.T) {
	mb := NewMessageBus(nil)

	if !mb.enabled {
		t.Fatal("Should be enabled by default")
	}

	mb.SetEnabled(false)
	if mb.enabled {
		t.Fatal("Should be disabled after SetEnabled(false)")
	}

	mb.SetEnabled(true)
	if !mb.enabled {
		t.Fatal("Should be enabled after SetEnabled(true)")
	}
}

func TestClear(t *testing.T) {
	mb := NewMessageBus(nil)

	mb.Send("pm", "msg1", "pm_message", PathPMToUser, "loc1", nil)
	mb.Send("se", "msg2", "se_stream", PathSEToUser, "loc2", nil)
	mb.Ack("some_id")

	mb.Clear()

	pending := mb.CheckPending()
	lost := mb.GetLostMessages()
	stats := mb.GetStats()

	if len(pending) != 0 || len(lost) != 0 || stats["received"].(int) != 0 {
		t.Fatalf("Clear should clear all state: pending=%d, lost=%d, received=%d",
			len(pending), len(lost), stats["received"])
	}
}

func TestPathConstants(t *testing.T) {
	expectedPaths := []MessagePath{
		PathPMToUser,
		PathPMStream,
		PathSEToUser,
		PathSEStream,
		PathSEExec,
		PathAPToUser,
		PathUserInput,
		PathSystem,
	}

	for _, path := range expectedPaths {
		if path == "" {
			t.Fatalf("Path constant should not be empty: %v", path)
		}
	}
}

func TestGetStats(t *testing.T) {
	mb := NewMessageBus(nil)

	stats := mb.GetStats()

	if stats["pending"].(int) != 0 {
		t.Fatalf("Initial pending should be 0, got %d", stats["pending"])
	}
	if stats["received"].(int) != 0 {
		t.Fatalf("Initial received should be 0, got %d", stats["received"])
	}
	if stats["lost"].(int) != 0 {
		t.Fatalf("Initial lost should be 0, got %d", stats["lost"])
	}

	mb.Send("pm", "test", "pm_message", PathPMToUser, "test_loc", nil)

	stats = mb.GetStats()
	if stats["pending"].(int) != 1 {
		t.Fatalf("After send, pending should be 1, got %d", stats["pending"])
	}
}

func BenchmarkSend(b *testing.B) {
	mb := NewMessageBus(nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mb.Send("pm", "benchmark content", "pm_message", PathPMToUser, "bench_loc", nil)
	}
}

func BenchmarkAck(b *testing.B) {
	mb := NewMessageBus(nil)

	ids := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		ids[i] = mb.Send("pm", "benchmark content", "pm_message", PathPMToUser, "bench_loc", nil)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mb.Ack(ids[i])
	}
}

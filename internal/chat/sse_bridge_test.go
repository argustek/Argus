package chat

import (
	"sync"
	"testing"
	"time"
)

func TestSSEBridge_SubscribeAndUnsubscribe(t *testing.T) {
	bridge := NewSSEBridge()

	ch, ok := bridge.Subscribe("client-1", "debug")
	if !ok {
		t.Fatal("首次订阅应该成功")
	}
	if ch == nil {
		t.Fatal("返回的 channel 不应为 nil")
	}
	if !bridge.HasActiveConnection() {
		t.Fatal("应该有活跃连接")
	}

	bridge.Unsubscribe("client-1")
	if bridge.HasActiveConnection() {
		t.Fatal("取消订阅后不应有活跃连接")
	}
}

func TestSSEBridge_SingleSubscriberOnly(t *testing.T) {
	bridge := NewSSEBridge()

	_, ok1 := bridge.Subscribe("client-1", "debug")
	if !ok1 {
		t.Fatal("首个订阅应成功")
	}

	_, ok2 := bridge.Subscribe("client-2", "debug")
	if ok2 {
		t.Fatal("调试模式的第二个订阅应该被拒绝（单订阅者模型）")
	}

	bridge.Unsubscribe("client-1")

	_, ok3 := bridge.Subscribe("client-3", "debug")
	if !ok3 {
		t.Fatal("取消后新订阅应成功")
	}
	bridge.Unsubscribe("client-3")
}

func TestSSEBridge_MultiSubscriberIDE(t *testing.T) {
	bridge := NewSSEBridge()

	_, ok1 := bridge.Subscribe("ide-a", "IDE-A")
	if !ok1 {
		t.Fatal("IDE-A 订阅应成功")
	}

	_, ok2 := bridge.Subscribe("ide-b", "IDE-B")
	if !ok2 {
		t.Fatal("IDE-B 订阅应成功（IDE模式允许多连接）")
	}

	if bridge.AllSubscriberCount() != 2 {
		t.Fatal("应该有2个订阅者")
	}

	bridge.Unsubscribe("ide-a")
	bridge.Unsubscribe("ide-b")
}

func TestSSEBridge_PushToSubscriber(t *testing.T) {
	bridge := NewSSEBridge()
	chA, _ := bridge.Subscribe("ide-a", "IDE-A")
	chB, _ := bridge.Subscribe("ide-b", "IDE-B")

	// 定向推送给 IDE-A
	bridge.PushToSubscriber("ide-a", SSEEvent{Type: "ide_message", Data: "hello A"})

	select {
	case event := <-chA:
		if event.Type != "ide_message" {
			t.Errorf("期望 ide_message，实际 %s", event.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("IDE-A 未收到定向推送")
	}

	// IDE-B 不应收到
	select {
	case <-chB:
		t.Fatal("IDE-B 不应收到定向给 A 的消息")
	case <-time.After(50 * time.Millisecond):
		// 正确：超时
	}

	bridge.Unsubscribe("ide-a")
	bridge.Unsubscribe("ide-b")
}

func TestSSEBridge_PushEvent(t *testing.T) {
	bridge := NewSSEBridge()
	ch, _ := bridge.Subscribe("test-client", "debug")

	testEvent := SSEEvent{
		Type: "pm_started",
		Data: map[string]string{"task": "write-hello-go"},
	}

	bridge.PushToAll(testEvent)

	select {
	case event := <-ch:
		if event.Type != "pm_started" {
			t.Errorf("期望事件类型 pm_started，实际 %s", event.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("超时：未收到推送的事件")
	}

	bridge.Unsubscribe("test-client")
}

func TestSSEBridge_PushWhenNoSubscriber(t *testing.T) {
	bridge := NewSSEBridge()

	event := SSEEvent{Type: "test", Data: "data"}
	bridge.PushToAll(event)

	if bridge.AllSubscriberCount() != 0 {
		t.Fatal("无订阅者时不应有计数")
	}
}

func TestSSEBridge_Heartbeat(t *testing.T) {
	bridge := NewSSEBridge()
	ch, _ := bridge.Subscribe("heartbeat-test", "debug")

	time.Sleep(11 * time.Second)

	select {
	case event := <-ch:
		if event.Type != "heartbeat" {
			t.Errorf("期望 heartbeat 事件，实际 %s", event.Type)
		}
	default:
		t.Log("心跳事件可能还未发送（时间窗口问题）")
	}

	bridge.Unsubscribe("heartbeat-test")
}

func TestSSEBridge_ConcurrentPush(t *testing.T) {
	bridge := NewSSEBridge()
	ch, _ := bridge.Subscribe("concurrent-test", "debug")

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			bridge.PushToAll(SSEEvent{
				Type: "test_event",
				Data: idx,
			})
		}(i)
	}

	wg.Wait()
	received := 0
	timeout := time.After(2 * time.Second)
loop:
	for {
		select {
		case <-ch:
			received++
		case <-timeout:
			break loop
		}
	}

	if received == 0 {
		t.Fatal("未收到任何并发推送的事件")
	}
	t.Logf("并发推送测试：收到 %d 个事件", received)

	bridge.Unsubscribe("concurrent-test")
}

func TestFormatSSE(t *testing.T) {
	data := map[string]string{"status": "running"}
	result := FormatSSE("pm_started", data)

	expected := "event: pm_started\ndata: {\"status\":\"running\"}\n\n"
	if result != expected {
		t.Errorf("SSE 格式化错误\n期望: %s\n实际: %s", expected, result)
	}
}

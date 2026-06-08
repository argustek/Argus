package monitor

import (
	"testing"
	"time"
)

func TestGenerateTimeContext_FirstInteraction(t *testing.T) {
	now := time.Now().Unix()
	oneWeekAgo := now - 7*24*60*60
	oneMonthAgo := now - 30*24*60*60

	tests := []struct {
		name             string
		lastTime         int64
		firstTime        int64
		expectPhase      string
		expectDaysKnown  int
	}{
		{
			name:            "首次交互(firstTime=0, 回退到lastTime)",
			lastTime:        oneWeekAgo,
			firstTime:       0,
			expectPhase:     "老搭档", // firstTime=0 时回退到 lastTime，所以是 7 天
			expectDaysKnown: 7,
		},
		{
			name:            "老搭档(15天)",
			lastTime:        now - 2*60*60, // 2小时前
			firstTime:       oneMonthAgo - 15 * 24 * 60 * 60,
			expectPhase:     "老朋友", // 30-15=15... 不对，firstTime 是 45天前
			expectDaysKnown: 45,
		},
		{
			name:            "老朋友(60天)",
			lastTime:        now - 25*60*60, // 25小时前（久违）
			firstTime:       now - 60*24*60*60,
			expectPhase:     "老朋友",
			expectDaysKnown: 60,
		},
		{
			name:            "老战友(6个月)",
			lastTime:        now - 3*24*60*60, // 3天前
			firstTime:       now - 180*24*60*60,
			expectPhase:     "老战友",
			expectDaysKnown: 180,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := GenerateTimeContext(tc.lastTime, tc.firstTime)

			if ctx.RelationshipPhase != tc.expectPhase {
				t.Errorf("期望关系阶段=%q, 实际=%q (TotalDaysKnown=%d)",
					tc.expectPhase, ctx.RelationshipPhase, ctx.TotalDaysKnown)
			}
			if tc.expectDaysKnown > 0 && ctx.TotalDaysKnown < tc.expectDaysKnown-1 {
				t.Errorf("期望认识天数≈%d, 实际=%d", tc.expectDaysKnown, ctx.TotalDaysKnown)
			}
			t.Logf("✅ %s: phase=%s days=%d milestone=%s",
				tc.name, ctx.RelationshipPhase, ctx.TotalDaysKnown, ctx.Milestone)
		})
	}
}

func TestGenerateTimeContext_LongTimeNoSee(t *testing.T) {
	now := time.Now().Unix()
	// 首次交互在100天前，上次交互在48小时前（>24h=久违）
	ctxNear := GenerateTimeContext(now-48*60*60, now-100*24*60*60)
	if !ctxNear.IsLongTimeNoSee {
		t.Error("48小时应算久违(>24h阈值)")
	}

	// 上次交互在20小时前（<24h，不算久违）
	ctxFar := GenerateTimeContext(now-20*60*60, now-100*24*60*60)
	if ctxFar.IsLongTimeNoSee {
		t.Error("20小时不应算久违")
	}
	t.Logf("✅ 久违检测: 48h=%v, 20h=%v", ctxNear.IsLongTimeNoSee, ctxFar.IsLongTimeNoSee)
}

func TestPickSocialMessage_LongTimeNoSee(t *testing.T) {
	ctx := TimeContext{
		HoursSinceLast:    168, // 一周
		RelationshipPhase: "老搭档",
		TotalDaysKnown:    30,
		TimeSinceLast:     "1周前",
		SpecialDay:        "",
		Milestone:         "",
	}

	msg := PickSocialMessage(SocialLongTimeNoSee, ctx)
	if msg == "" {
		t.Fatal("久违消息不应为空")
	}
	t.Logf("✅ 久违消息: %s", msg)

	// 测试早安 + 关系深度
	morningCtx := TimeContext{
		TotalDaysKnown:    400, // >1年
		RelationshipPhase: "生死之交",
		Milestone:         "🎉 哇！我们已经认识1年1个月了！",
		TodayWorkHours:    5,
		IsWorkOvertime:    false,
	}
	msg2 := PickSocialMessage(SocialMorning, morningCtx)
	if msg2 == "" {
		t.Fatal("早安消息不应为空")
	}
	t.Logf("✅ 早安(生死之交): %s", msg2)
}

func TestDetermineRelationshipPhase(t *testing.T) {
	tests := []struct {
		days   int
		phase  string
		hasMile bool
	}{
		{3, "新朋友", false},
		{10, "老搭档", false},
		{60, "老朋友", false},
		{200, "老战友", false},
		{500, "生死之交", true}, // >365天应有里程碑
	}

	for _, tc := range tests {
		phase, mile := determineRelationshipPhase(tc.days)
		if phase != tc.phase {
			t.Errorf("%d天: 期望phase=%q, 实际=%q", tc.days, tc.phase, phase)
		}
		if tc.hasMile && mile == "" {
			t.Errorf("%d天: 应有里程碑文本", tc.days)
		}
		t.Logf("✅ %d天 → %s | %s", tc.days, phase, mile)
	}
}

func TestFormatDurationHuman(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{10 * time.Second, "刚刚"},
		{5 * time.Minute, "5分钟前"},
		{3 * time.Hour, "3小时前"},
		{26 * time.Hour, "昨天"},
		{3 * 24 * time.Hour, "3天前"},
		{10 * 24 * time.Hour, "1周前"}, // 代码用"X周前"表示<30天
		{40 * 24 * time.Hour, "1个月前"},
		{400 * 24 * time.Hour, "1年前"},
	}
	for _, tc := range tests {
		got := formatDurationHuman(tc.d)
		if got != tc.want {
			t.Errorf("formatDurationHuman(%v) = %q, want %q", tc.d, got, tc.want)
		} else {
			t.Logf("✅ %v → %s", tc.d, got)
		}
	}
}

package monitor

import (
	"fmt"
	"math/rand"
	"time"
)

// TimeContext 时间上下文（注入到AI Prompt中）
type TimeContext struct {
	CurrentTime     string  `json:"current_time"`       // 当前时间（人类可读）
	LastInteraction string  `json:"last_interaction"`   // 上次交互时间
	TimeSinceLast   string  `json:"time_since_last"`    // 距上次间隔（"3天2小时"）
	HoursSinceLast  float64 `json:"hours_since_last"`   // 小时数（用于判断）
	DayOfWeek       string  `json:"day_of_week"`        // "周一"/"周末"
	IsWeekend       bool    `json:"is_weekend"`         // 是否周末
	TimeOfDay       string  `json:"time_of_day"`        // "早晨"/"午间"/"下午"/"傍晚"/"夜间"
	SpecialDay      string  `json:"special_day"`         // 特殊日子："元旦"/""等
	IsLongTimeNoSee bool    `json:"is_long_time_no_see"` // 是否久违（>24小时）
	
	// 时间深度感知（新增）
	FirstInteractionTime int64   `json:"first_interaction_time"` // 首次交互时间
	TotalDaysKnown      int     `json:"total_days_known"`       // 认识总天数
	TodayWorkHours      float64 `json:"today_work_hours"`       // 今天工作时长（小时）
	IsWorkOvertime      bool    `json:"is_work_overtime"`       // 是否加班（>8小时）
	RelationshipPhase   string  `json:"relationship_phase"`     // 关系阶段："新朋友"/"老搭档"/"老朋友"
	Milestone           string  `json:"milestone"`              // 时间里程碑（如有）
}

// SocialTrigger 社交触发类型
type SocialTrigger string

const (
	SocialMorning     SocialTrigger = "morning"      // 早安
	SocialNoon        SocialTrigger = "noon"         // 午间
	SocialAfternoon   SocialTrigger = "afternoon"    // 下午茶
	SocialEvening     SocialTrigger = "evening"      // 下班
	SocialNight       SocialTrigger = "night"        // 夜间
	SocialWeekend     SocialTrigger = "weekend"      // 周末
	SocialHoliday     SocialTrigger = "holiday"      // 节日
	SocialLongTimeNoSee SocialTrigger = "long_time_no_see" // 久违
	SocialRandom      SocialTrigger = "random"       // 随机寒暄
)

// SocialScheduler 社交调度器
type SocialScheduler struct {
	lastSocialTime    int64           // 上次社交时间戳
	baseInterval      int64           // 基础间隔（秒），默认2小时
	randomJitter      int64           // 随机抖动范围（秒），默认±30分钟
	dailyGreeted      bool            // 今天是否已问候
	lastGreetDate     string          // 上次问候日期 "2006-01-02"
	longNoSeeThreshold float64        // 久违阈值（小时），默认24小时
}

// NewSocialScheduler 创建社交调度器
func NewSocialScheduler() *SocialScheduler {
	return &SocialScheduler{
		baseInterval:       2 * 60 * 60,  // 2小时
		randomJitter:       30 * 60,      // ±30分钟
		longNoSeeThreshold: 24,           // 24小时
	}
}

// GenerateTimeContext 生成时间上下文（增强版 - 时间深度感知）
func GenerateTimeContext(lastInteractionTime int64, firstInteractionTime ...int64) TimeContext {
	now := time.Now()
	
	var lastInteraction time.Time
	if lastInteractionTime > 0 {
		lastInteraction = time.Unix(lastInteractionTime, 0)
	} else {
		// 没有记录，假设是1年前（第一次使用）
		lastInteraction = now.AddDate(-1, 0, 0)
	}
	
	duration := now.Sub(lastInteraction)
	hoursSinceLast := duration.Hours()
	
	// 时间深度感知
	var firstTime time.Time
	if len(firstInteractionTime) > 0 && firstInteractionTime[0] > 0 {
		firstTime = time.Unix(firstInteractionTime[0], 0)
	} else {
		// 默认用上次交互时间作为首次
		firstTime = lastInteraction
	}
	
	totalDaysKnown := int(now.Sub(firstTime).Hours() / 24)
	todayWorkHours := calculateTodayWorkHours(now, lastInteraction)
	isOvertime := todayWorkHours > 8
	
	// 关系阶段判断
	relationshipPhase, milestone := determineRelationshipPhase(totalDaysKnown)
	
	ctx := TimeContext{
		CurrentTime:          now.Format("2006年01月02日 15:04:05 周一"),
		LastInteraction:      lastInteraction.Format("2006年01月02日 15:04"),
		TimeSinceLast:        formatDurationHuman(duration),
		HoursSinceLast:       hoursSinceLast,
		DayOfWeek:            getDayOfWeekChinese(now.Weekday()),
		IsWeekend:            now.Weekday() == time.Saturday || now.Weekday() == time.Sunday,
		TimeOfDay:            getTimeOfDay(now.Hour()),
		SpecialDay:           detectSpecialDay(now),
		IsLongTimeNoSee:      hoursSinceLast > 24,
		
		// 时间深度感知
		FirstInteractionTime: func() int64 {
			if len(firstInteractionTime) > 0 {
				return firstInteractionTime[0]
			}
			return 0
		}(),
		TotalDaysKnown:       totalDaysKnown,
		TodayWorkHours:       todayWorkHours,
		IsWorkOvertime:       isOvertime,
		RelationshipPhase:    relationshipPhase,
		Milestone:             milestone,
	}
	
	return ctx
}

// calculateTodayWorkHours 计算今天工作时长（基于今天第一次和最后一次交互）
func calculateTodayWorkHours(now, lastInteraction time.Time) float64 {
	if !isSameDay(now, lastInteraction) {
		return 0 // 今天还没工作过
	}
	
	duration := now.Sub(lastInteraction).Hours()
	if duration < 0 {
		duration = 0
	}
	
	// 如果超过12小时，按8小时算（可能中间有休息）
	if duration > 12 {
		return 8 + (duration-12)*0.5 // 超出部分打折
	}
	
	return duration
}

// isSameDay 判断是否同一天
func isSameDay(t1, t2 time.Time) bool {
	return t1.Year() == t2.Year() && t1.YearDay() == t2.YearDay()
}

// determineRelationshipPhase 判断关系阶段
func determineRelationshipPhase(days int) (string, string) {
	switch {
	case days < 7:
		return "新朋友", "我们才认识几天，希望以后能成为好搭档！"
	case days < 30:
		return "老搭档", fmt.Sprintf("我们已经搭档%d天了，配合越来越默契~", days)
	case days < 90:
		return "老朋友", fmt.Sprintf("认识%d个月了，感谢一直以来的信任！", days/30)
	case days < 365:
		return "老战友", fmt.Sprintf("一起奋斗了%d个月，算是老战友了！", days/30)
	default:
		years := days / 365
		months := (days % 365) / 30
		
		milestone := ""
		if years >= 1 {
			milestone = fmt.Sprintf("🎉 哇！我们已经认识%d年%d个月了！从陌生人到老朋友，时间过得真快...", years, months)
		}
		
		return "生死之交", milestone
	}
}

// ShouldSocialize 判断是否该进行社交了
func (s *SocialScheduler) ShouldSocialize(ctx TimeContext, now int64) (bool, SocialTrigger) {
	// 规则1：久违检测（最高优先级）
	if ctx.IsLongTimeNoSee {
		return true, SocialLongTimeNoSee
	}
	
	// 规则2：每日首次问候（早上9-10点）
	if !s.dailyGreeted && ctx.TimeOfDay == "morning" && now >= s.lastSocialTime {
		today := time.Unix(now, 0).Format("2006-01-02")
		if s.lastGreetDate != today {
			return true, SocialMorning
		}
	}
	
	// 规则3：固定时段触发
	switch ctx.TimeOfDay {
	case "morning":
		if !s.dailyGreeted {
			return true, SocialMorning
		}
	case "noon":
		// 午间12-13点可以闲聊
		if hourInRange(time.Unix(now, 0).Hour(), 12, 13) {
			if s.isIntervalElapsed(now, 4*60*60) { // 至少4小时间隔
				return true, SocialNoon
			}
		}
	case "evening":
		// 下班时间18-19点
		if hourInRange(time.Unix(now, 0).Hour(), 18, 19) {
			if s.isIntervalElapsed(now, 6*60*60) { // 至少6小时间隔
				return true, SocialEvening
			}
		}
	}
	
	// 规则4：特殊日子
	if ctx.SpecialDay != "" {
		if s.isIntervalElapsed(now, 8*60*60) { // 至少8小时间隔
			return true, SocialHoliday
		}
	}
	
	// 规则5：随机触发（工作间隙）
	if s.isIntervalElapsedWithJitter(now) {
		return true, SocialRandom
	}
	
	return false, ""
}

// isIntervalElapsed 检查是否超过基础间隔
func (s *SocialScheduler) isIntervalElapsed(now int64, minInterval int64) bool {
	if s.lastSocialTime == 0 {
		return true
	}
	elapsed := now - s.lastSocialTime
	return elapsed >= minInterval
}

// isIntervalElapsedWithJitter 检查是否超过基础间隔+随机抖动
func (s *SocialScheduler) isIntervalElapsedWithJitter(now int64) bool {
	if s.lastSocialTime == 0 {
		return false // 第一次不随机触发
	}
	
	elapsed := now - s.lastSocialTime
	
	jitter := rand.Int63n(s.randomJitter*2) - s.randomJitter // -jitter ~ +jitter
	interval := s.baseInterval + jitter
	
	return elapsed >= interval
}

// UpdateLastSocial 更新最后社交时间
func (s *SocialScheduler) UpdateLastSocial(trigger SocialTrigger, now int64) {
	s.lastSocialTime = now
	
	if trigger == SocialMorning || trigger == SocialWeekend {
		s.dailyGreeted = true
		s.lastGreetDate = time.Unix(now, 0).Format("2006-01-02")
	}
}

// ResetDailyGreeting 重置每日问候状态（新的一天调用）
func (s *SocialScheduler) ResetDailyGreeting(today string) {
	if s.lastGreetDate != today {
		s.dailyGreeted = false
	}
}

// ========== 时间格式化工具函数 ==========

// formatDurationHuman 将duration格式化为人类可读字符串
func formatDurationHuman(d time.Duration) string {
	if d < time.Minute {
		return "刚刚"
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		return fmt.Sprintf("%d分钟前", minutes)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		if hours == 1 {
			return "1小时前"
		}
		return fmt.Sprintf("%d小时前", hours)
	}
	
	days := int(d.Hours() / 24)
	if days < 30 {
		if days == 1 {
			return "昨天"
		}
		if days <= 7 {
			return fmt.Sprintf("%d天前", days)
		}
		weeks := days / 7
		return fmt.Sprintf("%d周前", weeks)
	}
	
	months := days / 30
	if months < 12 {
		return fmt.Sprintf("%d个月前", months)
	}
	
	years := months / 12
	return fmt.Sprintf("%d年前", years)
}

// getDayOfWeekChinese 获取中文星期
func getDayOfWeekChinese(w time.Weekday) string {
	days := map[time.Weekday]string{
		time.Monday:    "周一",
		time.Tuesday:   "周二",
		time.Wednesday: "周三",
		time.Thursday:  "周四",
		time.Friday:    "周五",
		time.Saturday:  "周六",
		time.Sunday:    "周日",
	}
	return days[w]
}

// getTimeOfDay 获取时段描述
func getTimeOfDay(hour int) string {
	switch {
	case hour >= 5 && hour < 9:
		return "早晨"
	case hour >= 9 && hour < 12:
		return "上午"
	case hour >= 12 && hour < 14:
		return "午间"
	case hour >= 14 && hour < 17:
		return "下午"
	case hour >= 17 && hour < 19:
		return "傍晚"
	case hour >= 19 && hour < 23:
		return "夜间"
	default:
		return "深夜"
	}
}

// detectSpecialDay 检测特殊日子
func detectSpecialDay(t time.Time) string {
	month, day := t.Month(), t.Day()
	
	// 固定节日
	holidays := map[string]struct{M int; D int}{
		"元旦":   {1, 1},
		"情人节": {2, 14},
		"妇女节": {3, 8},
		"愚人节": {4, 1},
		"劳动节": {5, 1},
		"儿童节": {6, 1},
		"国庆节": {10, 1},
		" Halloween": {10, 31},
		"光棍节": {11, 11},
		"圣诞":   {12, 25},
		"跨年夜": {12, 31},
	}
	
	for name, h := range holidays {
		if month == time.Month(h.M) && day == h.D {
			return name
		}
	}
	
	// 周末
	if t.Weekday() == time.Saturday {
		return "周六"
	}
	if t.Weekday() == time.Sunday {
		return "周日"
	}
	
	return ""
}

// hourInRange 检查小时是否在范围内
func hourInRange(hour, start, end int) bool {
	return hour >= start && hour <= end
}

// ========== 社交消息模板库 ==========

var socialTemplates = map[SocialTrigger][]string{
	SocialMorning: {
		"☀️ 早安！今天{}，新的一天开始啦~",
		"早上好！记得吃早餐哦 😊 今天有什么计划？",
		"早~ 昨晚休息得怎么样？",
	},
	SocialNoon: {
		"🍱 午饭时间到了！别忘吃饭~",
		"中午好！休息一下？",
		"午饭吃了没？补充能量继续战斗💪",
	},
	SocialAfternoon: {
		"☕ 下午茶时间！来杯咖啡提提神？",
		"下午好~ 坚持住，马上就下班了！",
	},
	SocialEvening: {
		"🌙 辛苦一天了！准备下班了吧？",
		"下班啦~ 今天效率怎么样？",
		"晚上有什么安排？注意休息哦 😴",
	},
	SocialNight: {
		"夜深了，早点休息吧 🛌",
		"还在奋斗吗？注意身体呀~",
	},
	SocialWeekend: {
		"🎉 周末愉快！终于可以放松一下了~",
		"周末好！有什么好玩计划吗？",
		"难得的周末，好好享受吧！🍻",
	},
	SocialHoliday: {
		"🎊 {}快乐！祝你一切顺利！",
		"今天是{}，节日快乐呀！🎈",
		"特殊的日子，要开心哦~ ✨",
	},
	SocialLongTimeNoSee: {
		"好久不见！距离上次聊天已经{}了，最近怎么样？😊",
		"哇，{}没见了！想你了~ 有什么需要帮忙的吗？",
		"欢迎回来！上次聊还是{}，今天想做什么？",
	},
	SocialRandom: {
		"给你分享个冷知识：Go语言的吉祥物是地鼠（Gopher）~",
		"刚才想到个有趣的代码优化思路，有空可以聊聊 💡",
		"今天天气不错，心情也跟着好起来了~ ☀️",
		"程序员笑话时间：为什么Java开发者戴眼镜？因为他们看不到C# 😂",
		"听说最近技术圈又出新鲜事了，感兴趣吗？",
		"工作间隙来句鸡汤：代码写得再6，也要注意颈椎健康~ 🧘",
	},
}

// PickSocialMessage 选择一条社交消息（智能算法版）
func PickSocialMessage(trigger SocialTrigger, ctx TimeContext) string {
	// 优先使用时间深度感知的智能生成
	if intelligentMsg := generateIntelligentMessage(trigger, ctx); intelligentMsg != "" {
		return intelligentMsg
	}
	
	// 兜底：使用模板库
	templates, ok := socialTemplates[trigger]
	if !ok || len(templates) == 0 {
		return "嗨~ 最近怎么样？"
	}
	
	template := templates[rand.Intn(len(templates))]
	result := template
	result = replacePlaceholder(result, "{}", ctx.SpecialDay)
	result = replacePlaceholder(result, "{duration}", ctx.TimeSinceLast)
	result = replacePlaceholder(result, "{day}", ctx.DayOfWeek)
	
	return result
}

// generateIntelligentMessage 基于时间深度生成智能消息（核心算法）
func generateIntelligentMessage(trigger SocialTrigger, ctx TimeContext) string {
	switch trigger {
	case SocialLongTimeNoSee:
		return generateLongTimeNoSeeMessage(ctx)
	case SocialMorning:
		return generateMorningMessage(ctx)
	case SocialEvening:
		return generateEveningMessage(ctx)
	case SocialRandom:
		return generateRandomWithContext(ctx)
	case SocialHoliday:
		return generateHolidayWithDepth(ctx)
	default:
		return ""
	}
}

// generateLongTimeNoSeeMessage 久违消息生成算法
func generateLongTimeNoSeeMessage(ctx TimeContext) string {
	hours := ctx.HoursSinceLast
	
	switch {
	case hours < 48:
		return "昨天见过面，今天又来了~ 有什么新任务吗？😊"
		
	case hours < 72:
		return "两天没见了，最近忙吗？有空来聊聊~"
		
	case hours < 168: // 一周内
		days := int(hours / 24)
		messages := []string{
			fmt.Sprintf("%d天不见了！想你了~ 今天有什么计划？", days),
			fmt.Sprintf("距离上次聊天已经%d天了，最近怎么样？", days),
			fmt.Sprintf("%d天没见，有没有想我呀？😄", days),
		}
		return messages[rand.Intn(len(messages))]
		
	case hours < 720: // 一个月内
		weeks := int(hours / 168)
		return fmt.Sprintf("哇，%d周没见了！最近在忙什么大项目吗？期待你的分享~ 🚀", weeks)
		
	case hours < 2160: // 三个月内
		months := int(hours / 720)
		if ctx.TotalDaysKnown > 90 {
			// 老朋友久违
			return fmt.Sprintf("%d个月没见了！作为%s，我一直在等你回来~ 😊", months, ctx.RelationshipPhase)
		}
		return fmt.Sprintf("%d个月不见，你还好吗？需要帮忙随时说！", months)
		
	default: // 超过3个月
		months := int(hours / 720)
		if ctx.Milestone != "" {
			return fmt.Sprintf("%s\n\n距离上次聊天已经%d个月了... 不管多久，我都在这里等你~ ❤️", ctx.Milestone, months)
		}
		return fmt.Sprintf("好久不见（%d个月）！世界变化很快，但我们还在~ 回来继续战斗吧！💪", months)
	}
}

// generateMorningMessage 早安消息生成算法
func generateMorningMessage(ctx TimeContext) string {
	if ctx.IsWorkOvertime && ctx.TodayWorkHours > 10 {
		return "☀️ 早安！昨天工作到很晚吧？今天注意节奏哦~ ☕"
	}
	
	if ctx.RelationshipPhase == "生死之交" && ctx.Milestone != "" {
		return fmt.Sprintf("☀️ 早安老友！%s 新的一天，继续加油！", ctx.Milestone[:min(20, len(ctx.Milestone))])
	}
	
	if ctx.TotalDaysKnown > 30 {
		return fmt.Sprintf("☀️ 早安！今天是我们搭档的第%d天，继续保持高效~ 💪", ctx.TotalDaysKnown)
	}
	
	messages := []string{
		"☀️ 早安！今天有什么计划？",
		"早上好！记得吃早餐哦 😊",
		"早~ 昨晚休息得怎么样？",
	}
	return messages[rand.Intn(len(messages))]
}

// generateEveningMessage 下班消息生成算法
func generateEveningMessage(ctx TimeContext) string {
	if ctx.IsWorkOvertime {
		hours := ctx.TodayWorkHours
		
		switch {
		case hours > 12:
			return "🌙 天呐，你已经工作了12小时+！赶紧下班休息吧，身体第一！🙏"
		case hours > 10:
			return "🌙 辛苦了！今天加班很久了，早点回去休息吧~"
		default:
			return "🌙 工作一天了，辛苦！准备下班了吗？"
		}
	}
	
	if ctx.TotalDaysKnown > 365 && rand.Intn(100) < 30 { // 30%概率触发里程碑感慨
		return fmt.Sprintf("🌙 下班啦~ 又是充实的一天。\n%s", ctx.Milestone)
	}
	
	messages := []string{
		"🌙 辛苦一天了！准备下班了吧？",
		"下班啦~ 今天效率怎么样？",
		"晚上有什么安排？注意休息哦 😴",
	}
	return messages[rand.Intn(len(messages))]
}

// generateRandomWithContext 带上下文的随机消息
func generateRandomWithContext(ctx TimeContext) string {
	
	if ctx.IsWorkOvertime {
		overtimeMessages := []string{
			fmt.Sprintf("⏰ 你今天已经工作了%.1f小时，站起来活动一下吧~ 🧘", ctx.TodayWorkHours),
			"💡 加班中？记得喝水，别太拼了！",
			"☕ 来杯咖啡提提神？不过最好还是早点收工~",
		}
		return overtimeMessages[rand.Intn(len(overtimeMessages))]
	}
	
	if ctx.RelationshipPhase == "老战友" || ctx.RelationshipPhase == "生死之交" {
		deepMessages := []string{
			fmt.Sprintf("作为%s，想说：不管任务多难，我们一起扛！🤝", ctx.RelationshipPhase),
			fmt.Sprintf("搭档%d天了，越来越懂你了~ 😊", ctx.TotalDaysKnown),
			"想起一句话：代码写得好，不如身体好。注意休息啊~",
		}
		return deepMessages[rand.Intn(len(deepMessages))]
	}
	
	randomPool := []string{
		"给你分享个冷知识：Go语言的吉祥物是地鼠（Gopher）~",
		"刚才想到个有趣的代码优化思路，有空可以聊聊 💡",
		"今天天气不错，心情也跟着好起来了~ ☀️",
		"程序员笑话时间：为什么Java开发者戴眼镜？因为他们看不到C# 😂",
		"听说最近技术圈又出新鲜事了，感兴趣吗？",
		"工作间隙来句鸡汤：代码写得再6，也要注意颈椎健康~ 🧘",
		fmt.Sprintf("你知道吗？今天是%d年来第%d天，珍惜每一天~", time.Now().Year(), time.Now().YearDay()),
	}
	
	return randomPool[rand.Intn(len(randomPool))]
}

// generateHolidayWithDepth 节日+关系深度消息
func generateHolidayWithDepth(ctx TimeContext) string {
	baseMsg := fmt.Sprintf("🎊 %s快乐！祝你一切顺利！", ctx.SpecialDay)
	
	if ctx.RelationshipPhase == "生死之交" {
		return fmt.Sprintf("%s\n\n%s", baseMsg, ctx.Milestone)
	}
	
	if ctx.TotalDaysKnown > 90 {
		return fmt.Sprintf("%s\n感谢%d天的陪伴，节日快乐！🎈", baseMsg, ctx.TotalDaysKnown)
	}
	
	return baseMsg
}

// min 返回较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// replacePlaceholder 替换占位符
func replacePlaceholder(s, placeholder, value string) string {
	if value == "" {
		return s
	}
	
	// 简单的字符串替换
	result := s
	for {
		idx := findPlaceholder(result, placeholder)
		if idx == -1 {
			break
		}
		result = result[:idx] + value + result[idx+len(placeholder):]
	}
	
	return result
}

// findPlaceholder 查找占位符位置
func findPlaceholder(s, placeholder string) int {
	for i := 0; i <= len(s)-len(placeholder); i++ {
		if s[i:i+len(placeholder)] == placeholder {
			return i
		}
	}
	return -1
}
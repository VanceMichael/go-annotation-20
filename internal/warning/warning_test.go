package warning

import (
	"errors"
	"testing"

	"floodwatch/internal/model"
)

// 站点：警戒 22.0，保证 25.0，历史 26.5。超警幅度 3.0。
// 分档起点：蓝 22.0，黄 23.0，橙 24.0，红 25.0，历史 26.5。
func station() model.Station {
	return model.Station{
		Code: "HN-JLH-01", Name: "贾鲁河鄢陵站", River: "贾鲁河", Basin: model.BasinHuaihe,
		WarnLevel: 22.0, GuaranteeLevel: 25.0, HistoricLevel: 26.5, Online: true,
	}
}

// TestLevelForAtExactThresholds 断言水位恰好等于各档阈值时即进入该档。
func TestLevelForAtExactThresholds(t *testing.T) {
	s := station()
	cases := []struct {
		name  string
		level float64
		want  model.Level
	}{
		{"恰好达到警戒水位", 22.0, model.LevelBlue},
		{"恰好达到黄色阈值", 23.0, model.LevelYellow},
		{"恰好达到橙色阈值", 24.0, model.LevelOrange},
		{"恰好达到保证水位", 25.0, model.LevelRed},
		{"恰好达到历史最高", 26.5, model.LevelRed},
	}
	for _, tc := range cases {
		if got := LevelFor(tc.level, s); got != tc.want {
			t.Errorf("%s (%.3fm): 等级 = %s, 期望 %s", tc.name, tc.level, got, tc.want)
		}
	}
}

// TestLevelForJustBelowThresholds 断言略低于阈值时仍属下一档。
func TestLevelForJustBelowThresholds(t *testing.T) {
	s := station()
	cases := []struct {
		name  string
		level float64
		want  model.Level
	}{
		{"低于警戒水位 1 厘米", 21.99, model.LevelNone},
		{"低于黄色阈值 1 厘米", 22.99, model.LevelBlue},
		{"低于橙色阈值 1 厘米", 23.99, model.LevelYellow},
		{"低于保证水位 1 厘米", 24.99, model.LevelOrange},
	}
	for _, tc := range cases {
		if got := LevelFor(tc.level, s); got != tc.want {
			t.Errorf("%s (%.3fm): 等级 = %s, 期望 %s", tc.name, tc.level, got, tc.want)
		}
	}
}

// TestLevelForAboveThresholds 断言超过阈值时属于对应档位。
func TestLevelForAboveThresholds(t *testing.T) {
	s := station()
	cases := []struct {
		level float64
		want  model.Level
	}{
		{20.0, model.LevelNone},
		{22.5, model.LevelBlue},
		{23.4, model.LevelYellow},
		{24.6, model.LevelOrange},
		{25.8, model.LevelRed},
		{27.2, model.LevelRed},
	}
	for _, tc := range cases {
		if got := LevelFor(tc.level, s); got != tc.want {
			t.Errorf("%.3fm: 等级 = %s, 期望 %s", tc.level, got, tc.want)
		}
	}
}

// TestAssessAtGuaranteeLevelTriggersTopResponse 断言水位恰好达到保证水位时启动一级响应。
func TestAssessAtGuaranteeLevelTriggersTopResponse(t *testing.T) {
	s := station()
	a, err := Assess(25.0, s)
	if err != nil {
		t.Fatalf("Assess 返回错误: %v", err)
	}
	if a.Level != model.LevelRed {
		t.Fatalf("恰好达到保证水位 25.0m 时等级 = %s, 期望 %s", a.Level, model.LevelRed)
	}
	if a.Response != model.ResponseLevel1 {
		t.Fatalf("响应级别 = %s, 期望 %s", a.Response, model.ResponseLevel1)
	}
	if !a.Exceeded() {
		t.Fatalf("应判定为已超警戒")
	}
}

// TestAssessAtWarnLevelTriggersBlue 断言水位恰好达到警戒水位时进入蓝色预警。
func TestAssessAtWarnLevelTriggersBlue(t *testing.T) {
	s := station()
	a, err := Assess(22.0, s)
	if err != nil {
		t.Fatalf("Assess 返回错误: %v", err)
	}
	if a.Level != model.LevelBlue {
		t.Fatalf("恰好达到警戒水位 22.0m 时等级 = %s, 期望 %s", a.Level, model.LevelBlue)
	}
	if a.Response != model.ResponseLevel4 {
		t.Fatalf("响应级别 = %s, 期望 %s", a.Response, model.ResponseLevel4)
	}
	if a.ExceedM != 0 {
		t.Fatalf("恰好达到警戒水位时超警幅度 = %.3f, 期望 0", a.ExceedM)
	}
	if !a.Exceeded() {
		t.Fatalf("恰好达到警戒水位应判定为已超警戒")
	}
}

// TestAssessAtOrangeThreshold 断言恰好达到橙色阈值时进入二级响应。
func TestAssessAtOrangeThreshold(t *testing.T) {
	s := station()
	a, err := Assess(24.0, s)
	if err != nil {
		t.Fatalf("Assess 返回错误: %v", err)
	}
	if a.Level != model.LevelOrange {
		t.Fatalf("恰好达到橙色阈值 24.0m 时等级 = %s, 期望 %s", a.Level, model.LevelOrange)
	}
	if a.Response != model.ResponseLevel2 {
		t.Fatalf("响应级别 = %s, 期望 %s", a.Response, model.ResponseLevel2)
	}
	if a.UsedRatio <= 0.66 || a.UsedRatio >= 0.67 {
		t.Fatalf("超警占比 = %.3f, 期望约 0.667", a.UsedRatio)
	}
}

func TestAssessBelowWarnLevel(t *testing.T) {
	s := station()
	a, err := Assess(20.5, s)
	if err != nil {
		t.Fatalf("Assess 返回错误: %v", err)
	}
	if a.Level != model.LevelNone || a.Response != model.ResponseNone {
		t.Fatalf("判定 = %+v", a)
	}
	if a.Exceeded() {
		t.Fatalf("未超警戒不应判定为已超警戒")
	}
	if a.ExceedM != 0 || a.UsedRatio != 0 {
		t.Fatalf("未超警时超警幅度与占比应为 0: %+v", a)
	}
}

func TestAssessRejectsInvalidStation(t *testing.T) {
	s := station()
	s.GuaranteeLevel = s.WarnLevel // 保证水位不高于警戒水位
	if _, err := Assess(23, s); !errors.Is(err, model.ErrInvalidStation) {
		t.Fatalf("非法站点应返回 ErrInvalidStation, 实际 %v", err)
	}
}

// TestThresholdsMatchLevelFor 断言阈值表与等级判定完全一致。
func TestThresholdsMatchLevelFor(t *testing.T) {
	s := station()
	items, err := Thresholds(s)
	if err != nil {
		t.Fatalf("Thresholds 返回错误: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("阈值档数 = %d, 期望 4", len(items))
	}
	for _, th := range items {
		if got := LevelFor(th.FromM, s); got != th.Level {
			t.Errorf("阈值 %.3fm 判定为 %s, 阈值表标为 %s（阈值应达到即进入）", th.FromM, got, th.Level)
		}
	}
	for i := 1; i < len(items); i++ {
		if items[i].FromM <= items[i-1].FromM {
			t.Fatalf("阈值未递增: %+v", items)
		}
	}
}

func TestHighestAndEscalate(t *testing.T) {
	s := station()
	build := func(levels ...float64) []Assessment {
		out := make([]Assessment, 0, len(levels))
		for _, lv := range levels {
			a, err := Assess(lv, s)
			if err != nil {
				t.Fatalf("Assess 返回错误: %v", err)
			}
			out = append(out, a)
		}
		return out
	}

	if got := Highest(build(20.0, 22.5, 24.2)); got != model.LevelOrange {
		t.Errorf("最高等级 = %s, 期望 %s", got, model.LevelOrange)
	}
	if got := Highest(nil); got != model.LevelNone {
		t.Errorf("空输入最高等级 = %s, 期望 %s", got, model.LevelNone)
	}

	resp, err := Escalate(build(20.0, 25.0))
	if err != nil {
		t.Fatalf("Escalate 返回错误: %v", err)
	}
	if resp != model.ResponseLevel1 {
		t.Errorf("响应级别 = %s, 期望 %s", resp, model.ResponseLevel1)
	}
	if _, err := Escalate(nil); !errors.Is(err, model.ErrNoReadings) {
		t.Fatalf("空输入应返回 ErrNoReadings, 实际 %v", err)
	}
}

func TestCountByLevel(t *testing.T) {
	s := station()
	items := make([]Assessment, 0, 4)
	for _, lv := range []float64{20.0, 22.0, 23.5, 25.5} {
		a, err := Assess(lv, s)
		if err != nil {
			t.Fatalf("Assess 返回错误: %v", err)
		}
		items = append(items, a)
	}
	counts := CountByLevel(items)
	if counts["none"] != 1 || counts["blue"] != 1 || counts["yellow"] != 1 || counts["red"] != 1 {
		t.Fatalf("分档统计 = %+v", counts)
	}
	if counts["orange"] != 0 {
		t.Fatalf("orange 应为 0, 实际 %d", counts["orange"])
	}
}

func TestResponseForMapping(t *testing.T) {
	cases := map[model.Level]model.Response{
		model.LevelNone:   model.ResponseNone,
		model.LevelBlue:   model.ResponseLevel4,
		model.LevelYellow: model.ResponseLevel3,
		model.LevelOrange: model.ResponseLevel2,
		model.LevelRed:    model.ResponseLevel1,
	}
	for lv, want := range cases {
		if got := model.ResponseFor(lv); got != want {
			t.Errorf("ResponseFor(%s) = %s, 期望 %s", lv, got, want)
		}
	}
}

package reach

import (
	"errors"
	"math"
	"testing"
	"time"

	"floodwatch/internal/model"
)

// series 构造一段先涨后落、带一个尖刺的水位序列。
func series() []model.Reading {
	base := time.Date(2026, time.August, 16, 0, 0, 0, 0, time.UTC)
	levels := []float64{21.10, 21.45, 21.80, 23.90, 22.35, 22.70, 23.05, 23.40, 23.20, 22.95}
	rain := []float64{12.0, 18.5, 24.0, 31.5, 9.0, 6.5, 4.0, 2.5, 1.0, 0.5}
	out := make([]model.Reading, 0, len(levels))
	for i := range levels {
		out = append(out, model.Reading{
			StationCode: "HN-JLH-01",
			At:          base.Add(time.Duration(i) * time.Hour),
			LevelM:      levels[i],
			RainfallMM:  rain[i],
		})
	}
	return out
}

func clone(in []model.Reading) []model.Reading {
	out := make([]model.Reading, len(in))
	copy(out, in)
	return out
}

// TestSmoothDoesNotMutateInput 断言平滑不改动传入序列的顺序与取值。
func TestSmoothDoesNotMutateInput(t *testing.T) {
	for _, window := range []int{2, 3, 4, 5, 7} {
		in := series()
		before := clone(in)
		if _, err := Smooth(in, window); err != nil {
			t.Fatalf("window=%d: Smooth 返回错误: %v", window, err)
		}
		for i := range before {
			if in[i] != before[i] {
				t.Fatalf("window=%d: 第 %d 条观测被改动\n期望 %+v\n实际 %+v",
					window, i, before[i], in[i])
			}
		}
	}
}

// TestSmoothKeepsInputChronological 断言平滑后传入序列仍按时间递增。
func TestSmoothKeepsInputChronological(t *testing.T) {
	in := series()
	if !Chronological(in) {
		t.Fatalf("前提不成立: 构造的序列本应按时间递增")
	}
	if _, err := Smooth(in, 3); err != nil {
		t.Fatalf("Smooth 返回错误: %v", err)
	}
	if !Chronological(in) {
		t.Fatalf("平滑后传入序列的时间顺序被打乱: %+v", in)
	}
}

// TestSmoothSuppressesSpike 断言平滑能压掉尖刺且输出长度与输入一致。
func TestSmoothSuppressesSpike(t *testing.T) {
	in := series()
	out, err := Smooth(in, 3)
	if err != nil {
		t.Fatalf("Smooth 返回错误: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("平滑输出长度 = %d, 期望 %d", len(out), len(in))
	}
	// 原序列第 3 条是 23.90 的尖刺，平滑后应被邻域中位数压低。
	if out[3].LevelM >= 23.90 {
		t.Fatalf("尖刺未被平滑: 平滑后 = %.3f, 原值 23.90", out[3].LevelM)
	}
	for i := range out {
		if !out[i].At.Equal(in[i].At) {
			t.Fatalf("第 %d 条输出时刻 = %s, 期望 %s", i, out[i].At, in[i].At)
		}
	}
}

// TestMedianDoesNotMutateInput 断言中位数计算不改动传入切片。
func TestMedianDoesNotMutateInput(t *testing.T) {
	values := []float64{23.9, 21.1, 22.35, 21.8}
	before := append([]float64(nil), values...)
	got := Median(values)
	if math.Abs(got-22.075) > 1e-9 {
		t.Fatalf("中位数 = %.6f, 期望 22.075", got)
	}
	for i := range before {
		if values[i] != before[i] {
			t.Fatalf("Median 改动了传入切片: 期望 %v, 实际 %v", before, values)
		}
	}
	if Median(nil) != 0 {
		t.Fatalf("空输入中位数应为 0")
	}
	if got := Median([]float64{5}); got != 5 {
		t.Fatalf("单元素中位数 = %.2f, 期望 5", got)
	}
}

// TestAnalyseDoesNotMutateInput 断言完整分析不改动传入序列。
func TestAnalyseDoesNotMutateInput(t *testing.T) {
	in := series()
	before := clone(in)
	sum, err := Analyse(in, 3)
	if err != nil {
		t.Fatalf("Analyse 返回错误: %v", err)
	}
	for i := range before {
		if in[i] != before[i] {
			t.Fatalf("分析后第 %d 条观测被改动\n期望 %+v\n实际 %+v", i, before[i], in[i])
		}
	}
	if !sum.Chronological {
		t.Fatalf("分析摘要标记序列非时序, 但输入本应是时序的")
	}
}

// TestAnalyseRateUsesOriginalOrder 断言涨落率基于原始时间顺序计算。
func TestAnalyseRateUsesOriginalOrder(t *testing.T) {
	in := series()
	sum, err := Analyse(in, 3)
	if err != nil {
		t.Fatalf("Analyse 返回错误: %v", err)
	}
	// 首 21.10m，末 22.95m，历时 9 小时，涨落率约 +0.206 m/h。
	if sum.Rate.FromM != 21.10 {
		t.Fatalf("起始水位 = %.3f, 期望 21.10（应取序列首条）", sum.Rate.FromM)
	}
	if sum.Rate.ToM != 22.95 {
		t.Fatalf("结束水位 = %.3f, 期望 22.95（应取序列末条）", sum.Rate.ToM)
	}
	if sum.Rate.Hours != 9 {
		t.Fatalf("历时 = %.3f 小时, 期望 9", sum.Rate.Hours)
	}
	if !sum.Rate.Rising {
		t.Fatalf("序列整体上涨, Rising 应为 true")
	}
	if sum.Rate.RateMPerHour <= 0.2 || sum.Rate.RateMPerHour >= 0.21 {
		t.Fatalf("涨落率 = %.4f m/h, 期望约 0.206", sum.Rate.RateMPerHour)
	}
}

// TestAnalysePeakAndRainfall 断言峰值与累计雨量正确。
func TestAnalysePeakAndRainfall(t *testing.T) {
	in := series()
	sum, err := Analyse(in, 3)
	if err != nil {
		t.Fatalf("Analyse 返回错误: %v", err)
	}
	if sum.PeakM != 23.90 {
		t.Fatalf("峰值 = %.3f, 期望 23.90", sum.PeakM)
	}
	if sum.RainfallMM != 109.5 {
		t.Fatalf("累计雨量 = %.3f, 期望 109.5", sum.RainfallMM)
	}
	if sum.Count != 10 {
		t.Fatalf("条数 = %d, 期望 10", sum.Count)
	}
}

// TestRepeatedAnalyseIsStable 断言对同一序列重复分析结果一致。
func TestRepeatedAnalyseIsStable(t *testing.T) {
	in := series()
	first, err := Analyse(in, 3)
	if err != nil {
		t.Fatalf("首次 Analyse 失败: %v", err)
	}
	second, err := Analyse(in, 3)
	if err != nil {
		t.Fatalf("二次 Analyse 失败: %v", err)
	}
	if first.PeakM != second.PeakM {
		t.Fatalf("两次峰值不一致: %.3f vs %.3f", first.PeakM, second.PeakM)
	}
	if first.Rate != second.Rate {
		t.Fatalf("两次涨落率不一致: %+v vs %+v", first.Rate, second.Rate)
	}
}

func TestSmoothRejectsBadInput(t *testing.T) {
	if _, err := Smooth(series(), 0); err == nil {
		t.Errorf("窗口为 0 应返回错误")
	}
	if _, err := Smooth(nil, 3); !errors.Is(err, model.ErrNoReadings) {
		t.Errorf("空输入应返回 ErrNoReadings, 实际 %v", err)
	}
}

func TestRateRejectsShortSeries(t *testing.T) {
	if _, err := Rate(series()[:1]); !errors.Is(err, model.ErrNoReadings) {
		t.Fatalf("单条观测应返回 ErrNoReadings, 实际 %v", err)
	}
	base := time.Date(2026, time.August, 16, 0, 0, 0, 0, time.UTC)
	same := []model.Reading{
		{StationCode: "X", At: base, LevelM: 20},
		{StationCode: "X", At: base, LevelM: 21},
	}
	if _, err := Rate(same); !errors.Is(err, model.ErrInvalidReading) {
		t.Fatalf("时间未推进应返回 ErrInvalidReading, 实际 %v", err)
	}
}

func TestPeakEmpty(t *testing.T) {
	if _, err := Peak(nil); !errors.Is(err, model.ErrNoReadings) {
		t.Fatalf("空输入应返回 ErrNoReadings, 实际 %v", err)
	}
}

func TestChronologicalDetectsDisorder(t *testing.T) {
	in := series()
	if !Chronological(in) {
		t.Fatalf("有序序列应判定为时序")
	}
	in[2], in[5] = in[5], in[2]
	if Chronological(in) {
		t.Fatalf("乱序序列应判定为非时序")
	}
}

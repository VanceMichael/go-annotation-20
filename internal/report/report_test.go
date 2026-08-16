package report

import (
	"testing"

	"floodwatch/internal/model"
	"floodwatch/internal/seed"
)

func builder(t *testing.T) *Builder {
	t.Helper()
	reg, _, err := seed.Load()
	if err != nil {
		t.Fatalf("seed.Load 失败: %v", err)
	}
	return NewBuilder(reg)
}

// TestSituationAtGuaranteeLevelEscalatesToTop 断言有站点恰好达到保证水位时整体启动一级响应。
func TestSituationAtGuaranteeLevelEscalatesToTop(t *testing.T) {
	b := builder(t)
	sit, err := b.Situation(seed.Levels())
	if err != nil {
		t.Fatalf("Situation 返回错误: %v", err)
	}
	// 内置数据中 HN-JLH-01 水位 25.00 恰好等于保证水位 25.0。
	if sit.Highest != string(model.LevelRed) {
		t.Fatalf("最高等级 = %s, 期望 red（有站点恰好达到保证水位）", sit.Highest)
	}
	if sit.Response != string(model.ResponseLevel1) {
		t.Fatalf("响应级别 = %s, 期望 level-1", sit.Response)
	}
}

// TestSituationCountsStationsAtExactThresholds 断言恰好达到阈值的站点被计入对应档位。
func TestSituationCountsStationsAtExactThresholds(t *testing.T) {
	b := builder(t)
	sit, err := b.Situation(seed.Levels())
	if err != nil {
		t.Fatalf("Situation 返回错误: %v", err)
	}
	byCode := map[string]StationLine{}
	for _, l := range sit.Lines {
		byCode[l.Code] = l
	}

	// HN-JLH-01: 25.00 == 保证水位 25.0 -> 红色
	if got := byCode["HN-JLH-01"]; got.Level != string(model.LevelRed) {
		t.Errorf("HN-JLH-01 (%.2fm, 保证 %.2fm) 等级 = %s, 期望 red", got.LevelM, got.Guarantee, got.Level)
	}
	// HN-QYH-02: 19.50 == 警戒水位 19.5 -> 蓝色
	if got := byCode["HN-QYH-02"]; got.Level != string(model.LevelBlue) {
		t.Errorf("HN-QYH-02 (%.2fm, 警戒 %.2fm) 等级 = %s, 期望 blue", got.LevelM, got.WarnLevel, got.Level)
	}
	if got := byCode["HN-QYH-02"]; got.ExceedM != 0 {
		t.Errorf("HN-QYH-02 恰好达到警戒水位, 超警幅度应为 0, 实际 %.3f", got.ExceedM)
	}
	// ZJ-TXH-05: 4.10 < 警戒 4.5 -> 无预警
	if got := byCode["ZJ-TXH-05"]; got.Level != string(model.LevelNone) {
		t.Errorf("ZJ-TXH-05 等级 = %s, 期望 none", got.Level)
	}

	if sit.Exceeded != 5 {
		t.Errorf("超警站点数 = %d, 期望 5", sit.Exceeded)
	}
}

// TestSituationSkipsDischargeForUnratedStations 断言未建曲线的站点不参与流量统计且不导致报表失败。
func TestSituationSkipsDischargeForUnratedStations(t *testing.T) {
	b := builder(t)
	sit, err := b.Situation(seed.Levels())
	if err != nil {
		t.Fatalf("Situation 返回错误: %v", err)
	}
	byCode := map[string]StationLine{}
	for _, l := range sit.Lines {
		byCode[l.Code] = l
	}
	for _, code := range []string{"HN-QYH-02", "ZJ-TXH-05"} {
		got := byCode[code]
		if got.HasRating {
			t.Errorf("%s 未布设测流断面, HasRating 应为 false", code)
		}
		if got.DischargeM != 0 {
			t.Errorf("%s 无曲线, 流量应留 0, 实际 %.2f", code, got.DischargeM)
		}
	}
	for _, code := range []string{"HN-JLH-01", "SC-MJ-03", "SC-TJ-04"} {
		got := byCode[code]
		if !got.HasRating {
			t.Errorf("%s 应建有曲线", code)
		}
		if got.DischargeM <= 0 {
			t.Errorf("%s 流量 = %.2f, 应为正", code, got.DischargeM)
		}
	}
	if sit.WithRating != 4 {
		t.Errorf("建有曲线站点数 = %d, 期望 4", sit.WithRating)
	}
}

func TestSituationLineCount(t *testing.T) {
	b := builder(t)
	sit, err := b.Situation(seed.Levels())
	if err != nil {
		t.Fatalf("Situation 返回错误: %v", err)
	}
	if len(sit.Lines) != len(seed.Stations()) {
		t.Fatalf("报表行数 = %d, 期望 %d", len(sit.Lines), len(seed.Stations()))
	}
	var counted int
	for _, n := range sit.LevelCounts {
		counted += n
	}
	if counted != len(sit.Lines) {
		t.Fatalf("分档计数之和 = %d, 报表行数 = %d", counted, len(sit.Lines))
	}
	if sit.OnlineTotal != 5 {
		t.Fatalf("在线站点数 = %d, 期望 5", sit.OnlineTotal)
	}
}

func TestSituationPartialLevels(t *testing.T) {
	b := builder(t)
	sit, err := b.Situation(map[string]float64{"ZJ-TXH-05": 4.0})
	if err != nil {
		t.Fatalf("Situation 返回错误: %v", err)
	}
	if len(sit.Lines) != 1 {
		t.Fatalf("报表行数 = %d, 期望 1", len(sit.Lines))
	}
	if sit.Highest != string(model.LevelNone) {
		t.Fatalf("最高等级 = %s, 期望 none", sit.Highest)
	}
}

func TestBasinsFixedOrder(t *testing.T) {
	b := builder(t)
	lines, err := b.Basins(seed.Levels())
	if err != nil {
		t.Fatalf("Basins 返回错误: %v", err)
	}
	all := model.AllBasins()
	if len(lines) != len(all) {
		t.Fatalf("流域行数 = %d, 期望 %d", len(lines), len(all))
	}
	for i, bs := range all {
		if lines[i].Basin != string(bs) {
			t.Fatalf("第 %d 行流域 = %s, 期望 %s", i, lines[i].Basin, bs)
		}
	}
	byBasin := map[string]BasinLine{}
	for _, l := range lines {
		byBasin[l.Basin] = l
	}
	if got := byBasin[string(model.BasinHuaihe)]; got.Highest != string(model.LevelRed) {
		t.Errorf("淮河流域最高等级 = %s, 期望 red", got.Highest)
	}
	if got := byBasin[string(model.BasinYellow)]; got.Stations != 0 {
		t.Errorf("黄河流域站点数 = %d, 期望 0", got.Stations)
	}
	var stations int
	for _, l := range lines {
		stations += l.Stations
	}
	if stations != len(seed.Stations()) {
		t.Fatalf("流域站点数之和 = %d, 期望 %d", stations, len(seed.Stations()))
	}
}

// TestThresholdsReportCoversAllStations 断言阈值报表覆盖全部站点且每站四档。
func TestThresholdsReportCoversAllStations(t *testing.T) {
	b := builder(t)
	lines, err := b.Thresholds()
	if err != nil {
		t.Fatalf("Thresholds 返回错误: %v", err)
	}
	if len(lines) != len(seed.Stations()) {
		t.Fatalf("阈值报表行数 = %d, 期望 %d", len(lines), len(seed.Stations()))
	}
	for _, l := range lines {
		if len(l.Thresholds) != 4 {
			t.Fatalf("站点 %s 阈值档数 = %d, 期望 4", l.Code, len(l.Thresholds))
		}
	}
}

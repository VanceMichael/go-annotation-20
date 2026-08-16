package model

import (
	"errors"
	"testing"
	"time"
)

func validStation() Station {
	return Station{
		Code: "HN-JLH-01", Name: "贾鲁河鄢陵站", River: "贾鲁河", Basin: BasinHuaihe,
		WarnLevel: 22.0, GuaranteeLevel: 25.0, HistoricLevel: 26.5, Online: true,
	}
}

func TestParseBasin(t *testing.T) {
	for _, b := range AllBasins() {
		got, err := ParseBasin(string(b))
		if err != nil || got != b {
			t.Fatalf("ParseBasin(%q) = %q, %v", b, got, err)
		}
		if b.DisplayName() == "" {
			t.Errorf("%s 缺少中文名", b)
		}
	}
	if _, err := ParseBasin("  HUAIHE "); err != nil {
		t.Fatalf("应忽略大小写与空白: %v", err)
	}
	if _, err := ParseBasin("nile"); !errors.Is(err, ErrUnknownBasin) {
		t.Fatalf("未知流域应返回 ErrUnknownBasin, 实际 %v", err)
	}
	if Basin("x").DisplayName() != "x" {
		t.Errorf("未知流域应回落为原值")
	}
}

func TestParseLevelAndRank(t *testing.T) {
	prev := -1
	for _, l := range AllLevels() {
		got, err := ParseLevel(string(l))
		if err != nil || got != l {
			t.Fatalf("ParseLevel(%q) = %q, %v", l, got, err)
		}
		if l.Rank() <= prev && l != LevelNone {
			t.Fatalf("%s 序号 %d 应递增（上一档 %d）", l, l.Rank(), prev)
		}
		prev = l.Rank()
		if l.DisplayName() == "" {
			t.Errorf("%s 缺少中文名", l)
		}
	}
	if _, err := ParseLevel("purple"); !errors.Is(err, ErrUnknownLevel) {
		t.Fatalf("未知等级应返回 ErrUnknownLevel, 实际 %v", err)
	}
	if Level("x").Rank() != 0 {
		t.Errorf("未知等级序号应为 0")
	}
	if Level("x").DisplayName() != "x" {
		t.Errorf("未知等级应回落为原值")
	}
}

func TestResponseForAndDisplay(t *testing.T) {
	cases := map[Level]Response{
		LevelNone:   ResponseNone,
		LevelBlue:   ResponseLevel4,
		LevelYellow: ResponseLevel3,
		LevelOrange: ResponseLevel2,
		LevelRed:    ResponseLevel1,
	}
	for lv, want := range cases {
		if got := ResponseFor(lv); got != want {
			t.Errorf("ResponseFor(%s) = %s, 期望 %s", lv, got, want)
		}
		if want.DisplayName() == "" {
			t.Errorf("%s 缺少中文名", want)
		}
	}
	if Response("x").DisplayName() != "x" {
		t.Errorf("未知响应级别应回落为原值")
	}
}

func TestStationValidate(t *testing.T) {
	if err := validStation().Validate(); err != nil {
		t.Fatalf("合法站点不应报错: %v", err)
	}
	mutations := []func(s *Station){
		func(s *Station) { s.Code = "" },
		func(s *Station) { s.Name = " " },
		func(s *Station) { s.Basin = "nile" },
		func(s *Station) { s.WarnLevel = 0 },
		func(s *Station) { s.GuaranteeLevel = s.WarnLevel },
		func(s *Station) { s.GuaranteeLevel = s.WarnLevel - 1 },
		func(s *Station) { s.HistoricLevel = s.GuaranteeLevel - 0.5 },
	}
	for i, mutate := range mutations {
		s := validStation()
		mutate(&s)
		if err := s.Validate(); !errors.Is(err, ErrInvalidStation) {
			t.Errorf("第 %d 项非法输入应返回 ErrInvalidStation, 实际 %v", i, err)
		}
	}
}

// TestHasRating 断言曲线存在性判定基于指针是否为 nil。
func TestHasRating(t *testing.T) {
	s := validStation()
	if s.HasRating() {
		t.Errorf("未设置曲线时 HasRating 应为 false")
	}
	s.Rating = &RatingCurve{Coefficient: 1, Exponent: 1, ZeroLevel: 0}
	if !s.HasRating() {
		t.Errorf("设置曲线后 HasRating 应为 true")
	}
}

func TestReadingValidate(t *testing.T) {
	base := Reading{
		StationCode: "HN-JLH-01",
		At:          time.Date(2026, time.August, 16, 0, 0, 0, 0, time.UTC),
		LevelM:      22.5, RainfallMM: 12,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("合法记录不应报错: %v", err)
	}
	mutations := []func(r *Reading){
		func(r *Reading) { r.StationCode = "" },
		func(r *Reading) { r.At = time.Time{} },
		func(r *Reading) { r.RainfallMM = -1 },
	}
	for i, mutate := range mutations {
		r := base
		mutate(&r)
		if err := r.Validate(); !errors.Is(err, ErrInvalidReading) {
			t.Errorf("第 %d 项非法输入应返回 ErrInvalidReading, 实际 %v", i, err)
		}
	}
}

func TestSortReadings(t *testing.T) {
	t0 := time.Date(2026, time.August, 16, 0, 0, 0, 0, time.UTC)
	items := []Reading{
		{StationCode: "B", At: t0.Add(time.Hour)},
		{StationCode: "Z", At: t0},
		{StationCode: "A", At: t0},
	}
	SortReadings(items)
	if items[0].StationCode != "A" || items[1].StationCode != "Z" || items[2].StationCode != "B" {
		t.Fatalf("排序结果 = %+v", items)
	}
}

func TestBreachStateTerminalAndDisplay(t *testing.T) {
	terminal := map[BreachState]bool{BreachClosed: true, BreachAbandoned: true}
	states := []BreachState{BreachReported, BreachSurveyed, BreachClosing, BreachClosed, BreachAbandoned}
	for _, s := range states {
		if got := s.Terminal(); got != terminal[s] {
			t.Errorf("%s.Terminal() = %v, 期望 %v", s, got, terminal[s])
		}
		if s.DisplayName() == "" {
			t.Errorf("%s 缺少中文名", s)
		}
	}
	if BreachState("x").DisplayName() != "x" {
		t.Errorf("未知状态应回落为原值")
	}
}

func TestBreachDuration(t *testing.T) {
	start := time.Date(2026, time.August, 16, 6, 0, 0, 0, time.UTC)
	b := Breach{ReportedAt: start, ClosedAt: start.Add(14 * time.Hour)}
	if got := b.Duration(); got != 14*time.Hour {
		t.Errorf("Duration = %v, 期望 14h", got)
	}
	if got := (Breach{ReportedAt: start}).Duration(); got != 0 {
		t.Errorf("未合龙 Duration = %v, 期望 0", got)
	}
	if got := (Breach{}).Duration(); got != 0 {
		t.Errorf("零值 Duration = %v, 期望 0", got)
	}
}

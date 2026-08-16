package gauge

import (
	"errors"
	"math"
	"testing"

	"floodwatch/internal/model"
)

func rated() model.Station {
	return model.Station{
		Code: "HN-JLH-01", Name: "贾鲁河鄢陵站", River: "贾鲁河", Basin: model.BasinHuaihe,
		WarnLevel: 22.0, GuaranteeLevel: 25.0, HistoricLevel: 26.5, Online: true,
		Rating: &model.RatingCurve{Coefficient: 48.5, Exponent: 1.62, ZeroLevel: 18.0},
	}
}

// unrated 是未布设测流断面的站点，没有水位流量关系曲线。
func unrated() model.Station {
	return model.Station{
		Code: "HN-QYH-02", Name: "清潩河仓头站", River: "清潩河", Basin: model.BasinHuaihe,
		WarnLevel: 19.5, GuaranteeLevel: 21.8, HistoricLevel: 22.4, Online: true,
		Rating: nil,
	}
}

// TestDischargeOnUnratedStationReturnsSentinel 断言未建曲线的站点推算流量时
// 返回 model.ErrNoRatingCurve，而不是 panic。
func TestDischargeOnUnratedStationReturnsSentinel(t *testing.T) {
	s := unrated()
	got, err := Discharge(s, 20.5)
	if err == nil {
		t.Fatalf("未建曲线的站点应返回错误, 实际返回流量 %.2f", got)
	}
	if !errors.Is(err, model.ErrNoRatingCurve) {
		t.Fatalf("errors.Is(err, model.ErrNoRatingCurve) = false, 错误为 %v", err)
	}
}

// TestDischargeOnUnratedStationAcrossLevels 断言未建曲线的站点在任意水位下都不 panic。
func TestDischargeOnUnratedStationAcrossLevels(t *testing.T) {
	s := unrated()
	for _, level := range []float64{0, 10.0, 19.5, 21.8, 30.0, -5.0} {
		got, err := Discharge(s, level)
		if err == nil {
			t.Fatalf("水位 %.2f: 未建曲线的站点应返回错误, 实际 %.2f", level, got)
		}
		if !errors.Is(err, model.ErrNoRatingCurve) {
			t.Fatalf("水位 %.2f: 错误 = %v, 期望 ErrNoRatingCurve", level, err)
		}
	}
}

// TestRegistryDischargeAtUnratedStation 断言经台账查询未建曲线站点同样返回哨兵错误。
func TestRegistryDischargeAtUnratedStation(t *testing.T) {
	r := New()
	if err := r.AddStation(rated()); err != nil {
		t.Fatalf("AddStation 失败: %v", err)
	}
	if err := r.AddStation(unrated()); err != nil {
		t.Fatalf("AddStation 失败: %v", err)
	}

	if _, err := r.DischargeAt("HN-QYH-02", 20.5); !errors.Is(err, model.ErrNoRatingCurve) {
		t.Fatalf("未建曲线站点应返回 ErrNoRatingCurve, 实际 %v", err)
	}
	if got, err := r.DischargeAt("HN-JLH-01", 22.0); err != nil || got <= 0 {
		t.Fatalf("建有曲线站点应返回正流量, 实际 %.2f, %v", got, err)
	}
	if _, err := r.DischargeAt("NOPE", 20); !errors.Is(err, model.ErrStationUnknown) {
		t.Fatalf("未知站点应返回 ErrStationUnknown, 实际 %v", err)
	}
}

// TestHasRating 断言曲线存在性判定正确。
func TestHasRating(t *testing.T) {
	if !rated().HasRating() {
		t.Errorf("建有曲线的站点 HasRating 应为 true")
	}
	if unrated().HasRating() {
		t.Errorf("未建曲线的站点 HasRating 应为 false")
	}
	if (model.Station{}).HasRating() {
		t.Errorf("零值站点 HasRating 应为 false")
	}
}

func TestDischargeBelowZeroLevel(t *testing.T) {
	s := rated()
	got, err := Discharge(s, s.Rating.ZeroLevel)
	if err != nil {
		t.Fatalf("Discharge 返回错误: %v", err)
	}
	if got != 0 {
		t.Fatalf("断流水位处流量 = %.2f, 期望 0", got)
	}
	got, err = Discharge(s, s.Rating.ZeroLevel-2)
	if err != nil {
		t.Fatalf("Discharge 返回错误: %v", err)
	}
	if got != 0 {
		t.Fatalf("低于断流水位流量 = %.2f, 期望 0", got)
	}
}

func TestDischargeMonotonic(t *testing.T) {
	s := rated()
	prev := -1.0
	for _, level := range []float64{18.5, 19.0, 20.0, 22.0, 25.0, 26.5} {
		got, err := Discharge(s, level)
		if err != nil {
			t.Fatalf("Discharge 返回错误: %v", err)
		}
		if got <= prev {
			t.Fatalf("水位 %.2f 流量 %.2f 应大于更低水位的 %.2f", level, got, prev)
		}
		prev = got
	}
}

func TestDischargeFormula(t *testing.T) {
	s := rated()
	got, err := Discharge(s, 22.0)
	if err != nil {
		t.Fatalf("Discharge 返回错误: %v", err)
	}
	want := math.Round(48.5*math.Pow(4.0, 1.62)*100) / 100
	if math.Abs(got-want) > 0.01 {
		t.Fatalf("流量 = %.2f, 期望 %.2f", got, want)
	}
}

func TestRatingStations(t *testing.T) {
	r := New()
	if err := r.AddStation(rated()); err != nil {
		t.Fatalf("AddStation 失败: %v", err)
	}
	if err := r.AddStation(unrated()); err != nil {
		t.Fatalf("AddStation 失败: %v", err)
	}
	got := r.RatingStations()
	if len(got) != 1 || got[0] != "HN-JLH-01" {
		t.Fatalf("建有曲线的站码 = %v, 期望 [HN-JLH-01]", got)
	}
}

func TestOnlineStation(t *testing.T) {
	r := New()
	s := unrated()
	s.Online = false
	if err := r.AddStation(s); err != nil {
		t.Fatalf("AddStation 失败: %v", err)
	}
	if _, err := r.OnlineStation(s.Code); !errors.Is(err, model.ErrStationOffline) {
		t.Fatalf("离线站点应返回 ErrStationOffline, 实际 %v", err)
	}
	if _, err := r.Station(s.Code); err != nil {
		t.Fatalf("离线站点仍应可查询: %v", err)
	}
}

func TestAddStationValidation(t *testing.T) {
	r := New()
	bad := rated()
	bad.GuaranteeLevel = bad.WarnLevel
	if err := r.AddStation(bad); !errors.Is(err, model.ErrInvalidStation) {
		t.Fatalf("保证水位不高于警戒水位应返回 ErrInvalidStation, 实际 %v", err)
	}
	bad2 := rated()
	bad2.HistoricLevel = bad2.GuaranteeLevel - 1
	if err := r.AddStation(bad2); !errors.Is(err, model.ErrInvalidStation) {
		t.Fatalf("历史水位低于保证水位应返回 ErrInvalidStation, 实际 %v", err)
	}
}

func TestReachRegistration(t *testing.T) {
	r := New()
	if err := r.AddStation(rated()); err != nil {
		t.Fatalf("AddStation 失败: %v", err)
	}
	if err := r.AddStation(unrated()); err != nil {
		t.Fatalf("AddStation 失败: %v", err)
	}
	reach := model.Reach{
		Code: "R-01", Name: "贾鲁河中段", UpstreamCode: "HN-JLH-01",
		DownstreamCode: "HN-QYH-02", Basin: model.BasinHuaihe, LengthKM: 42.5, LeveeGrade: 2,
	}
	if err := r.AddReach(reach); err != nil {
		t.Fatalf("AddReach 失败: %v", err)
	}
	if _, err := r.Reach("R-01"); err != nil {
		t.Fatalf("Reach 失败: %v", err)
	}
	if _, err := r.Reach("NOPE"); !errors.Is(err, model.ErrReachUnknown) {
		t.Fatalf("未知河段应返回 ErrReachUnknown, 实际 %v", err)
	}

	bad := reach
	bad.Code = "R-02"
	bad.UpstreamCode = "MISSING"
	if err := r.AddReach(bad); !errors.Is(err, model.ErrStationUnknown) {
		t.Fatalf("断面站点不存在应返回 ErrStationUnknown, 实际 %v", err)
	}
}

func TestCountsAndBasinFilter(t *testing.T) {
	r := New()
	if err := r.AddStation(rated()); err != nil {
		t.Fatalf("AddStation 失败: %v", err)
	}
	off := unrated()
	off.Online = false
	if err := r.AddStation(off); err != nil {
		t.Fatalf("AddStation 失败: %v", err)
	}
	c := r.Counts()
	if c.Stations != 2 || c.Online != 1 || c.WithRating != 1 {
		t.Fatalf("Counts = %+v", c)
	}
	if got := r.StationsByBasin(model.BasinHuaihe); len(got) != 2 {
		t.Fatalf("淮河流域站点数 = %d, 期望 2", len(got))
	}
	if got := r.StationsByBasin(model.BasinYangtze); len(got) != 0 {
		t.Fatalf("长江流域站点数 = %d, 期望 0", len(got))
	}
}

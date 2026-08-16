// Package seed 提供内置的水位站、河段、抢险队伍与观测样例数据。
//
// 样例取材于 2026 年汛期多地强降雨的典型场景，仅用于本地演练。
package seed

import (
	"fmt"
	"time"

	"floodwatch/internal/breach"
	"floodwatch/internal/gauge"
	"floodwatch/internal/model"
)

// Stations 返回内置水位站清单。
//
// 其中 HN-QYH-02、ZJ-TXH-05 未布设测流断面，没有水位流量关系曲线。
func Stations() []model.Station {
	return []model.Station{
		{
			Code: "HN-JLH-01", Name: "贾鲁河鄢陵站", River: "贾鲁河", Basin: model.BasinHuaihe,
			WarnLevel: 22.0, GuaranteeLevel: 25.0, HistoricLevel: 26.5, Online: true,
			Rating: &model.RatingCurve{Coefficient: 48.5, Exponent: 1.62, ZeroLevel: 18.0},
		},
		{
			Code: "HN-QYH-02", Name: "清潩河仓头站", River: "清潩河", Basin: model.BasinHuaihe,
			WarnLevel: 19.5, GuaranteeLevel: 21.8, HistoricLevel: 22.4, Online: true,
			Rating: nil,
		},
		{
			Code: "SC-MJ-03", Name: "岷江彭山站", River: "岷江", Basin: model.BasinYangtze,
			WarnLevel: 31.5, GuaranteeLevel: 34.2, HistoricLevel: 35.8, Online: true,
			Rating: &model.RatingCurve{Coefficient: 96.0, Exponent: 1.48, ZeroLevel: 27.0},
		},
		{
			Code: "SC-TJ-04", Name: "沱江富顺站", River: "沱江", Basin: model.BasinYangtze,
			WarnLevel: 28.0, GuaranteeLevel: 30.5, HistoricLevel: 31.9, Online: true,
			Rating: &model.RatingCurve{Coefficient: 72.5, Exponent: 1.55, ZeroLevel: 24.0},
		},
		{
			Code: "ZJ-TXH-05", Name: "苕溪湖州站", River: "东苕溪", Basin: model.BasinSoutheast,
			WarnLevel: 4.5, GuaranteeLevel: 5.6, HistoricLevel: 6.1, Online: true,
			Rating: nil,
		},
		{
			Code: "GD-BJ-06", Name: "北江石角站", River: "北江", Basin: model.BasinPearl,
			WarnLevel: 9.8, GuaranteeLevel: 11.6, HistoricLevel: 12.7, Online: false,
			Rating: &model.RatingCurve{Coefficient: 118.0, Exponent: 1.41, ZeroLevel: 5.5},
		},
	}
}

// Reaches 返回内置河段清单。
func Reaches() []model.Reach {
	return []model.Reach{
		{Code: "R-JLH-01", Name: "贾鲁河中段", UpstreamCode: "HN-JLH-01", DownstreamCode: "HN-QYH-02",
			Basin: model.BasinHuaihe, LengthKM: 42.5, LeveeGrade: 2},
		{Code: "R-MJ-02", Name: "岷江下段", UpstreamCode: "SC-MJ-03", DownstreamCode: "SC-TJ-04",
			Basin: model.BasinYangtze, LengthKM: 88.0, LeveeGrade: 1},
		{Code: "R-TXH-03", Name: "东苕溪下段", UpstreamCode: "ZJ-TXH-05", DownstreamCode: "ZJ-TXH-05",
			Basin: model.BasinSoutheast, LengthKM: 26.4, LeveeGrade: 3},
	}
}

// Crews 返回内置抢险队伍清单。
func Crews() []model.Crew {
	return []model.Crew{
		{ID: "C-01", Name: "省级机动抢险队", Base: "郑州", Headcount: 120, CapacityM: 18, Standby: true},
		{ID: "C-02", Name: "市级抢险队", Base: "许昌", Headcount: 60, CapacityM: 10, Standby: true},
		{ID: "C-03", Name: "县级抢险队", Base: "鄢陵", Headcount: 40, CapacityM: 6, Standby: true},
		{ID: "C-04", Name: "流域机动抢险队", Base: "成都", Headcount: 96, CapacityM: 14, Standby: true},
		{ID: "C-05", Name: "水利工程抢险队", Base: "湖州", Headcount: 52, CapacityM: 8, Standby: false},
	}
}

// Levels 返回内置的各站当前水位，单位米。
func Levels() map[string]float64 {
	return map[string]float64{
		"HN-JLH-01": 25.00, // 恰好达到保证水位
		"HN-QYH-02": 19.50, // 恰好达到警戒水位
		"SC-MJ-03":  32.40, // 黄色区间
		"SC-TJ-04":  29.25, // 橙色区间
		"ZJ-TXH-05": 4.10,  // 未超警
		"GD-BJ-06":  10.30, // 离线站
	}
}

// Readings 返回内置的贾鲁河鄢陵站观测序列。
func Readings() []model.Reading {
	base := time.Date(2026, time.August, 16, 0, 0, 0, 0, time.UTC)
	levels := []float64{21.10, 21.45, 21.80, 23.90, 22.35, 22.70, 23.05, 23.40, 24.60, 25.00}
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

// TotalRainfallMM 返回内置观测序列的累计雨量，供自检使用。
func TotalRainfallMM() float64 {
	var sum float64
	for _, r := range Readings() {
		sum += r.RainfallMM
	}
	return sum
}

// Load 构造带内置样例数据的台账与抢险服务。
func Load() (*gauge.Registry, *breach.Service, error) {
	reg := gauge.New()
	for _, s := range Stations() {
		if err := reg.AddStation(s); err != nil {
			return nil, nil, fmt.Errorf("seed: 登记水位站 %s 失败: %w", s.Code, err)
		}
	}
	for _, x := range Reaches() {
		if err := reg.AddReach(x); err != nil {
			return nil, nil, fmt.Errorf("seed: 登记河段 %s 失败: %w", x.Code, err)
		}
	}
	svc := breach.NewService(reg)
	for _, c := range Crews() {
		if err := svc.AddCrew(c); err != nil {
			return nil, nil, fmt.Errorf("seed: 登记抢险队伍 %s 失败: %w", c.ID, err)
		}
	}
	return reg, svc, nil
}

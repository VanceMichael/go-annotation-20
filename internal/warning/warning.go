// Package warning 实现防汛预警等级判定。
//
// 判定口径按水位相对特征水位的位置划分，各档均为「达到即进入」，
// 即水位恰好等于某档阈值时就属于该档：
//
//	水位 >= 历史最高水位            红色预警
//	水位 >= 保证水位                红色预警
//	水位 >= 警戒水位 + 2/3 超警幅度  橙色预警
//	水位 >= 警戒水位 + 1/3 超警幅度  黄色预警
//	水位 >= 警戒水位                蓝色预警
//	其余                            无预警
//
// 超警幅度指保证水位与警戒水位之差。
package warning

import (
	"fmt"
	"sort"

	"floodwatch/internal/model"
)

// Assessment 是一次预警判定结果。
type Assessment struct {
	StationCode string         `json:"station_code"`
	StationName string         `json:"station_name"`
	LevelM      float64        `json:"level_m"`
	WarnLevel   float64        `json:"warn_level"`
	Guarantee   float64        `json:"guarantee_level"`
	Historic    float64        `json:"historic_level"`
	Level       model.Level    `json:"level"`
	LevelName   string         `json:"level_name"`
	Response    model.Response `json:"response"`
	// ExceedM 是超警幅度，未超警时为 0。
	ExceedM float64 `json:"exceed_m"`
	// UsedRatio 是水位在超警区间中的占比，未超警时为 0。
	UsedRatio float64 `json:"used_ratio"`
}

// Exceeded 报告是否已超警戒水位。
func (a Assessment) Exceeded() bool {
	return a.Level.Rank() >= model.LevelBlue.Rank()
}

// LevelFor 依据水位与站点特征水位判定预警等级。
//
// 各档均为「达到即进入」：水位恰好等于阈值时属于该档。
func LevelFor(levelM float64, s model.Station) model.Level {
	span := s.GuaranteeLevel - s.WarnLevel
	switch {
	case levelM >= s.HistoricLevel:
		return model.LevelRed
	case levelM >= s.GuaranteeLevel:
		return model.LevelRed
	case levelM >= s.WarnLevel+span*2/3:
		return model.LevelOrange
	case levelM >= s.WarnLevel+span/3:
		return model.LevelYellow
	case levelM >= s.WarnLevel:
		return model.LevelBlue
	default:
		return model.LevelNone
	}
}

// Assess 生成一次完整的预警判定。
func Assess(levelM float64, s model.Station) (Assessment, error) {
	if err := s.Validate(); err != nil {
		return Assessment{}, err
	}
	lv := LevelFor(levelM, s)
	a := Assessment{
		StationCode: s.Code,
		StationName: s.Name,
		LevelM:      levelM,
		WarnLevel:   s.WarnLevel,
		Guarantee:   s.GuaranteeLevel,
		Historic:    s.HistoricLevel,
		Level:       lv,
		LevelName:   lv.DisplayName(),
		Response:    model.ResponseFor(lv),
	}
	if levelM >= s.WarnLevel {
		a.ExceedM = round3(levelM - s.WarnLevel)
		span := s.GuaranteeLevel - s.WarnLevel
		if span > 0 {
			a.UsedRatio = round3(a.ExceedM / span)
		}
	}
	return a, nil
}

// Thresholds 返回某站各档预警的起始水位，按等级由低到高。
type Threshold struct {
	Level model.Level `json:"level"`
	FromM float64     `json:"from_m"`
	Name  string      `json:"level_name"`
}

// Thresholds 返回某站的分档阈值表。
func Thresholds(s model.Station) ([]Threshold, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	span := s.GuaranteeLevel - s.WarnLevel
	out := []Threshold{
		{model.LevelBlue, round3(s.WarnLevel), model.LevelBlue.DisplayName()},
		{model.LevelYellow, round3(s.WarnLevel + span/3), model.LevelYellow.DisplayName()},
		{model.LevelOrange, round3(s.WarnLevel + span*2/3), model.LevelOrange.DisplayName()},
		{model.LevelRed, round3(s.GuaranteeLevel), model.LevelRed.DisplayName()},
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FromM < out[j].FromM })
	return out, nil
}

// Highest 返回一组判定中的最高等级。
func Highest(items []Assessment) model.Level {
	highest := model.LevelNone
	for _, a := range items {
		if a.Level.Rank() > highest.Rank() {
			highest = a.Level
		}
	}
	return highest
}

// Escalate 依据一组判定给出流域整体应急响应级别。
func Escalate(items []Assessment) (model.Response, error) {
	if len(items) == 0 {
		return model.ResponseNone, fmt.Errorf("%w: 无可用判定", model.ErrNoReadings)
	}
	return model.ResponseFor(Highest(items)), nil
}

// CountByLevel 统计各等级的站点数量。
func CountByLevel(items []Assessment) map[string]int {
	out := make(map[string]int)
	for _, l := range model.AllLevels() {
		out[string(l)] = 0
	}
	for _, a := range items {
		out[string(a.Level)]++
	}
	return out
}

func round3(v float64) float64 {
	return float64(int64(v*1000+sign(v)*0.5)) / 1000
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// Package report 生成防汛水情统计报表。
package report

import (
	"fmt"
	"sort"

	"floodwatch/internal/gauge"
	"floodwatch/internal/model"
	"floodwatch/internal/warning"
)

// StationLine 是水位站维度的报表行。
type StationLine struct {
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	River      string  `json:"river"`
	Basin      string  `json:"basin"`
	Online     bool    `json:"online"`
	LevelM     float64 `json:"level_m"`
	WarnLevel  float64 `json:"warn_level"`
	Guarantee  float64 `json:"guarantee_level"`
	Level      string  `json:"level"`
	LevelName  string  `json:"level_name"`
	Response   string  `json:"response"`
	ExceedM    float64 `json:"exceed_m"`
	HasRating  bool    `json:"has_rating"`
	DischargeM float64 `json:"discharge_m3s"`
}

// Situation 是流域水情态势报表。
type Situation struct {
	Lines       []StationLine  `json:"stations"`
	LevelCounts map[string]int `json:"level_counts"`
	BasinCounts map[string]int `json:"basin_counts"`
	Highest     string         `json:"highest_level"`
	Response    string         `json:"response"`
	Exceeded    int            `json:"exceeded"`
	WithRating  int            `json:"with_rating"`
	OnlineTotal int            `json:"online"`
}

// Builder 组装报表所需的数据源。
type Builder struct {
	registry *gauge.Registry
}

// NewBuilder 构造报表生成器。
func NewBuilder(reg *gauge.Registry) *Builder {
	return &Builder{registry: reg}
}

// Situation 依据各站当前水位生成态势报表。
//
// 未建水位流量关系曲线的站点不参与流量统计，其 discharge 字段留 0，
// 不因缺少曲线而使整份报表失败。
func (b *Builder) Situation(levels map[string]float64) (Situation, error) {
	out := Situation{
		LevelCounts: map[string]int{},
		BasinCounts: map[string]int{},
	}
	for _, l := range model.AllLevels() {
		out.LevelCounts[string(l)] = 0
	}

	assessments := make([]warning.Assessment, 0, len(levels))
	for _, s := range b.registry.Stations() {
		levelM, ok := levels[s.Code]
		if !ok {
			continue
		}
		a, err := warning.Assess(levelM, s)
		if err != nil {
			return Situation{}, fmt.Errorf("report: 站点 %s 预警判定失败: %w", s.Code, err)
		}
		assessments = append(assessments, a)

		line := StationLine{
			Code: s.Code, Name: s.Name, River: s.River, Basin: string(s.Basin),
			Online: s.Online, LevelM: levelM, WarnLevel: s.WarnLevel, Guarantee: s.GuaranteeLevel,
			Level: string(a.Level), LevelName: a.LevelName, Response: string(a.Response),
			ExceedM: a.ExceedM, HasRating: s.HasRating(),
		}
		if s.HasRating() {
			q, derr := gauge.Discharge(s, levelM)
			if derr != nil {
				return Situation{}, fmt.Errorf("report: 站点 %s 流量推算失败: %w", s.Code, derr)
			}
			line.DischargeM = q
			out.WithRating++
		}
		if s.Online {
			out.OnlineTotal++
		}
		if a.Exceeded() {
			out.Exceeded++
		}
		out.LevelCounts[string(a.Level)]++
		out.BasinCounts[string(s.Basin)]++
		out.Lines = append(out.Lines, line)
	}

	sort.Slice(out.Lines, func(i, j int) bool { return out.Lines[i].Code < out.Lines[j].Code })
	highest := warning.Highest(assessments)
	out.Highest = string(highest)
	out.Response = string(model.ResponseFor(highest))
	return out, nil
}

// BasinLine 是流域维度的报表行。
type BasinLine struct {
	Basin     string `json:"basin"`
	BasinName string `json:"basin_name"`
	Stations  int    `json:"stations"`
	Exceeded  int    `json:"exceeded"`
	Highest   string `json:"highest_level"`
	Response  string `json:"response"`
}

// Basins 生成流域维度报表，顺序固定。
func (b *Builder) Basins(levels map[string]float64) ([]BasinLine, error) {
	byBasin := make(map[model.Basin]*BasinLine)
	highestOf := make(map[model.Basin]model.Level)
	for _, bs := range model.AllBasins() {
		byBasin[bs] = &BasinLine{Basin: string(bs), BasinName: bs.DisplayName()}
		highestOf[bs] = model.LevelNone
	}

	for _, s := range b.registry.Stations() {
		levelM, ok := levels[s.Code]
		if !ok {
			continue
		}
		line, exists := byBasin[s.Basin]
		if !exists {
			continue
		}
		a, err := warning.Assess(levelM, s)
		if err != nil {
			return nil, fmt.Errorf("report: 站点 %s 预警判定失败: %w", s.Code, err)
		}
		line.Stations++
		if a.Exceeded() {
			line.Exceeded++
		}
		if a.Level.Rank() > highestOf[s.Basin].Rank() {
			highestOf[s.Basin] = a.Level
		}
	}

	out := make([]BasinLine, 0, len(byBasin))
	for _, bs := range model.AllBasins() {
		line := *byBasin[bs]
		line.Highest = string(highestOf[bs])
		line.Response = string(model.ResponseFor(highestOf[bs]))
		out = append(out, line)
	}
	return out, nil
}

// ThresholdLine 是分档阈值报表行。
type ThresholdLine struct {
	Code       string              `json:"code"`
	Name       string              `json:"name"`
	Thresholds []warning.Threshold `json:"thresholds"`
}

// Thresholds 生成全部站点的分档阈值报表。
func (b *Builder) Thresholds() ([]ThresholdLine, error) {
	stations := b.registry.Stations()
	out := make([]ThresholdLine, 0, len(stations))
	for _, s := range stations {
		items, err := warning.Thresholds(s)
		if err != nil {
			return nil, fmt.Errorf("report: 站点 %s 阈值计算失败: %w", s.Code, err)
		}
		out = append(out, ThresholdLine{Code: s.Code, Name: s.Name, Thresholds: items})
	}
	return out, nil
}

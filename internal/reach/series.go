// Package reach 实现河段水位序列的分析处理。
//
// 序列处理函数（中位数平滑、涨落率、峰值检测）都是只读分析：
// 传入的观测序列必须保持原有的时间顺序与取值，不得被原地改动。
package reach

import (
	"fmt"
	"math"
	"sort"

	"floodwatch/internal/model"
)

// Median 返回一组水位的中位数。
// 该函数不会改动传入的切片。
func Median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	buf := make([]float64, len(values))
	copy(buf, values)
	sort.Float64s(buf)
	mid := len(buf) / 2
	if len(buf)%2 == 1 {
		return buf[mid]
	}
	return (buf[mid-1] + buf[mid]) / 2
}

// Smooth 对水位序列做滑动中位数平滑，窗口长度为 window。
//
// 该函数是只读分析：返回新的序列，传入的 readings 必须保持原有顺序与取值。
func Smooth(readings []model.Reading, window int) ([]model.Reading, error) {
	if window <= 0 {
		return nil, fmt.Errorf("reach: 平滑窗口必须为正, 收到 %d", window)
	}
	if len(readings) == 0 {
		return nil, fmt.Errorf("%w: 平滑输入为空", model.ErrNoReadings)
	}
	out := make([]model.Reading, 0, len(readings))
	for i := range readings {
		lo := i - window/2
		if lo < 0 {
			lo = 0
		}
		hi := lo + window
		if hi > len(readings) {
			hi = len(readings)
		}
		levels := make([]float64, 0, hi-lo)
		for _, r := range readings[lo:hi] {
			levels = append(levels, r.LevelM)
		}
		smoothed := readings[i]
		smoothed.LevelM = round3(Median(levels))
		out = append(out, smoothed)
	}
	return out, nil
}

// RateResult 是涨落率计算结果。
type RateResult struct {
	StationCode string  `json:"station_code"`
	FromM       float64 `json:"from_m"`
	ToM         float64 `json:"to_m"`
	Hours       float64 `json:"hours"`
	// RateMPerHour 是涨落率，正为上涨，单位米每小时。
	RateMPerHour float64 `json:"rate_m_per_hour"`
	Rising       bool    `json:"rising"`
}

// Rate 计算序列首末之间的涨落率。
//
// 该函数依赖 readings 的时间顺序，且不会改动传入的切片。
func Rate(readings []model.Reading) (RateResult, error) {
	if len(readings) < 2 {
		return RateResult{}, fmt.Errorf("%w: 涨落率至少需要 2 条观测", model.ErrNoReadings)
	}
	first, last := readings[0], readings[len(readings)-1]
	hours := last.At.Sub(first.At).Hours()
	if hours <= 0 {
		return RateResult{}, fmt.Errorf("%w: 序列时间未推进（首 %s, 末 %s）",
			model.ErrInvalidReading, first.At.Format("15:04"), last.At.Format("15:04"))
	}
	rate := (last.LevelM - first.LevelM) / hours
	return RateResult{
		StationCode:  first.StationCode,
		FromM:        first.LevelM,
		ToM:          last.LevelM,
		Hours:        round3(hours),
		RateMPerHour: round3(rate),
		Rising:       rate > 0,
	}, nil
}

// Peak 返回序列中的最高水位观测。
// 该函数不会改动传入的切片。
func Peak(readings []model.Reading) (model.Reading, error) {
	if len(readings) == 0 {
		return model.Reading{}, fmt.Errorf("%w: 峰值输入为空", model.ErrNoReadings)
	}
	best := readings[0]
	for _, r := range readings[1:] {
		if r.LevelM > best.LevelM {
			best = r
		}
	}
	return best, nil
}

// TotalRainfall 返回序列累计降雨量。
func TotalRainfall(readings []model.Reading) float64 {
	var sum float64
	for _, r := range readings {
		sum += r.RainfallMM
	}
	return round3(sum)
}

// Chronological 报告序列是否按时间非递减排列。
func Chronological(readings []model.Reading) bool {
	for i := 1; i < len(readings); i++ {
		if readings[i].At.Before(readings[i-1].At) {
			return false
		}
	}
	return true
}

// Summary 是一段序列的分析摘要。
type Summary struct {
	StationCode   string          `json:"station_code"`
	Count         int             `json:"count"`
	PeakM         float64         `json:"peak_m"`
	PeakAt        string          `json:"peak_at"`
	RainfallMM    float64         `json:"rainfall_mm"`
	Rate          RateResult      `json:"rate"`
	Chronological bool            `json:"chronological"`
	Smoothed      []model.Reading `json:"-"`
}

// Analyse 对序列做完整分析：平滑、峰值、涨落率与累计雨量。
//
// 分析全过程是只读的，返回后传入的 readings 必须与调用前完全一致。
func Analyse(readings []model.Reading, window int) (Summary, error) {
	if len(readings) == 0 {
		return Summary{}, fmt.Errorf("%w: 分析输入为空", model.ErrNoReadings)
	}
	smoothed, err := Smooth(readings, window)
	if err != nil {
		return Summary{}, err
	}
	peak, err := Peak(readings)
	if err != nil {
		return Summary{}, err
	}
	rate, err := Rate(readings)
	if err != nil {
		return Summary{}, err
	}
	return Summary{
		StationCode:   readings[0].StationCode,
		Count:         len(readings),
		PeakM:         peak.LevelM,
		PeakAt:        peak.At.Format("2006-01-02 15:04"),
		RainfallMM:    TotalRainfall(readings),
		Rate:          rate,
		Chronological: Chronological(readings),
		Smoothed:      smoothed,
	}, nil
}

func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}

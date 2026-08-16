// Package gauge 维护水位站台账并推算水情要素。
//
// 只有布设了测流断面的水位站才建有水位流量关系曲线（Station.Rating）。
// 未建曲线的站点无法推算流量，相关查询必须返回 model.ErrNoRatingCurve。
package gauge

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"floodwatch/internal/model"
)

// Registry 是线程安全的水位站台账。
type Registry struct {
	mu       sync.RWMutex
	stations map[string]model.Station
	reaches  map[string]model.Reach
}

// New 构造空台账。
func New() *Registry {
	return &Registry{
		stations: make(map[string]model.Station),
		reaches:  make(map[string]model.Reach),
	}
}

// AddStation 登记水位站。
func (r *Registry) AddStation(s model.Station) error {
	if err := s.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stations[s.Code] = s
	return nil
}

// Station 返回水位站。
func (r *Registry) Station(code string) (model.Station, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.stations[code]
	if !ok {
		return model.Station{}, fmt.Errorf("%w: %s", model.ErrStationUnknown, code)
	}
	return s, nil
}

// OnlineStation 返回在线水位站。
func (r *Registry) OnlineStation(code string) (model.Station, error) {
	s, err := r.Station(code)
	if err != nil {
		return model.Station{}, err
	}
	if !s.Online {
		return model.Station{}, fmt.Errorf("%w: %s", model.ErrStationOffline, code)
	}
	return s, nil
}

// Stations 返回全部水位站，按站码排序。
func (r *Registry) Stations() []model.Station {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]model.Station, 0, len(r.stations))
	for _, s := range r.stations {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// StationsByBasin 返回指定流域的水位站。
func (r *Registry) StationsByBasin(b model.Basin) []model.Station {
	all := r.Stations()
	out := make([]model.Station, 0, len(all))
	for _, s := range all {
		if s.Basin == b {
			out = append(out, s)
		}
	}
	return out
}

// AddReach 登记河段。
func (r *Registry) AddReach(x model.Reach) error {
	if strings.TrimSpace(x.Code) == "" {
		return fmt.Errorf("gauge: 河段代码为空")
	}
	if x.LengthKM <= 0 {
		return fmt.Errorf("gauge: 河段 %s 长度必须为正", x.Code)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.stations[x.UpstreamCode]; !ok {
		return fmt.Errorf("%w: 河段 %s 的上断面 %s", model.ErrStationUnknown, x.Code, x.UpstreamCode)
	}
	if _, ok := r.stations[x.DownstreamCode]; !ok {
		return fmt.Errorf("%w: 河段 %s 的下断面 %s", model.ErrStationUnknown, x.Code, x.DownstreamCode)
	}
	r.reaches[x.Code] = x
	return nil
}

// Reach 返回河段。
func (r *Registry) Reach(code string) (model.Reach, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	x, ok := r.reaches[code]
	if !ok {
		return model.Reach{}, fmt.Errorf("%w: %s", model.ErrReachUnknown, code)
	}
	return x, nil
}

// Reaches 返回全部河段，按代码排序。
func (r *Registry) Reaches() []model.Reach {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]model.Reach, 0, len(r.reaches))
	for _, x := range r.reaches {
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// Discharge 依据水位流量关系曲线推算流量，单位立方米每秒。
//
// 未建曲线的站点返回 model.ErrNoRatingCurve。
func Discharge(s model.Station, levelM float64) (float64, error) {
	if !s.HasRating() {
		return 0, fmt.Errorf("%w: %s", model.ErrNoRatingCurve, s.Code)
	}
	c := s.Rating
	if levelM <= c.ZeroLevel {
		return 0, nil
	}
	return round2(c.Coefficient * math.Pow(levelM-c.ZeroLevel, c.Exponent)), nil
}

// DischargeAt 查询某站在给定水位下的流量。
func (r *Registry) DischargeAt(code string, levelM float64) (float64, error) {
	s, err := r.Station(code)
	if err != nil {
		return 0, err
	}
	return Discharge(s, levelM)
}

// RatingStations 返回建有水位流量关系曲线的站码，按字典序排序。
func (r *Registry) RatingStations() []string {
	all := r.Stations()
	out := make([]string, 0, len(all))
	for _, s := range all {
		if s.HasRating() {
			out = append(out, s.Code)
		}
	}
	sort.Strings(out)
	return out
}

// Counts 汇总台账规模。
type Counts struct {
	Stations   int `json:"stations"`
	Online     int `json:"online"`
	WithRating int `json:"with_rating"`
	Reaches    int `json:"reaches"`
}

// Counts 返回台账规模统计。
func (r *Registry) Counts() Counts {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c := Counts{Stations: len(r.stations), Reaches: len(r.reaches)}
	for _, s := range r.stations {
		if s.Online {
			c.Online++
		}
		if s.HasRating() {
			c.WithRating++
		}
	}
	return c
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

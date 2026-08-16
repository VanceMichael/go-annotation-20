// Package model 定义防汛水情监测与溃口抢险调度平台的领域模型。
//
// 平台面向流域防汛，覆盖水位站观测、水情要素计算、预警等级判定、
// 河段断面分析、溃口抢险力量调度与水旱灾害防御应急响应管理。
package model

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Basin 表示流域。
type Basin string

const (
	// BasinHuaihe 淮河流域。
	BasinHuaihe Basin = "huaihe"
	// BasinYangtze 长江流域。
	BasinYangtze Basin = "yangtze"
	// BasinYellow 黄河流域。
	BasinYellow Basin = "yellow"
	// BasinPearl 珠江流域。
	BasinPearl Basin = "pearl"
	// BasinSoutheast 东南诸河。
	BasinSoutheast Basin = "southeast"
)

// AllBasins 返回全部流域。
func AllBasins() []Basin {
	return []Basin{BasinHuaihe, BasinYangtze, BasinYellow, BasinPearl, BasinSoutheast}
}

// DisplayName 返回流域中文名。
func (b Basin) DisplayName() string {
	switch b {
	case BasinHuaihe:
		return "淮河流域"
	case BasinYangtze:
		return "长江流域"
	case BasinYellow:
		return "黄河流域"
	case BasinPearl:
		return "珠江流域"
	case BasinSoutheast:
		return "东南诸河"
	default:
		return string(b)
	}
}

// ParseBasin 解析流域代码。
func ParseBasin(s string) (Basin, error) {
	v := Basin(strings.ToLower(strings.TrimSpace(s)))
	for _, b := range AllBasins() {
		if v == b {
			return v, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownBasin, s)
}

// Level 表示防汛预警等级，由低到高。
type Level string

const (
	// LevelNone 无预警。
	LevelNone Level = "none"
	// LevelBlue 蓝色预警。
	LevelBlue Level = "blue"
	// LevelYellow 黄色预警。
	LevelYellow Level = "yellow"
	// LevelOrange 橙色预警。
	LevelOrange Level = "orange"
	// LevelRed 红色预警。
	LevelRed Level = "red"
)

// AllLevels 返回全部预警等级，由低到高。
func AllLevels() []Level {
	return []Level{LevelNone, LevelBlue, LevelYellow, LevelOrange, LevelRed}
}

// DisplayName 返回预警等级中文名。
func (l Level) DisplayName() string {
	switch l {
	case LevelNone:
		return "无预警"
	case LevelBlue:
		return "蓝色预警"
	case LevelYellow:
		return "黄色预警"
	case LevelOrange:
		return "橙色预警"
	case LevelRed:
		return "红色预警"
	default:
		return string(l)
	}
}

// Rank 返回等级序号，数字越大越严重。
func (l Level) Rank() int {
	switch l {
	case LevelBlue:
		return 1
	case LevelYellow:
		return 2
	case LevelOrange:
		return 3
	case LevelRed:
		return 4
	default:
		return 0
	}
}

// ParseLevel 解析预警等级代码。
func ParseLevel(s string) (Level, error) {
	v := Level(strings.ToLower(strings.TrimSpace(s)))
	for _, l := range AllLevels() {
		if v == l {
			return v, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownLevel, s)
}

// Response 表示水旱灾害防御应急响应级别。
type Response string

const (
	// ResponseNone 未启动响应。
	ResponseNone Response = "none"
	// ResponseLevel4 四级响应。
	ResponseLevel4 Response = "level-4"
	// ResponseLevel3 三级响应。
	ResponseLevel3 Response = "level-3"
	// ResponseLevel2 二级响应。
	ResponseLevel2 Response = "level-2"
	// ResponseLevel1 一级响应。
	ResponseLevel1 Response = "level-1"
)

// DisplayName 返回响应级别中文名。
func (r Response) DisplayName() string {
	switch r {
	case ResponseNone:
		return "未启动响应"
	case ResponseLevel4:
		return "四级响应"
	case ResponseLevel3:
		return "三级响应"
	case ResponseLevel2:
		return "二级响应"
	case ResponseLevel1:
		return "一级响应"
	default:
		return string(r)
	}
}

// ResponseFor 依据预警等级映射应急响应级别。
func ResponseFor(l Level) Response {
	switch l {
	case LevelRed:
		return ResponseLevel1
	case LevelOrange:
		return ResponseLevel2
	case LevelYellow:
		return ResponseLevel3
	case LevelBlue:
		return ResponseLevel4
	default:
		return ResponseNone
	}
}

// RatingCurve 是水位流量关系曲线。
//
// 并非所有水位站都建有水位流量关系曲线：只有布设了测流断面的站点才有。
// 未建曲线的站点该字段为 nil，此时无法推算流量。
type RatingCurve struct {
	// Coefficient 是流量系数。
	Coefficient float64 `json:"coefficient"`
	// Exponent 是水位差指数。
	Exponent float64 `json:"exponent"`
	// ZeroLevel 是断流水位，低于该水位流量按 0 计。
	ZeroLevel float64 `json:"zero_level"`
}

// Station 表示一个水位站。
type Station struct {
	Code string `json:"code"`
	Name string `json:"name"`
	// River 是所在河流名称。
	River string `json:"river"`
	Basin Basin  `json:"basin"`
	// WarnLevel 是警戒水位，单位米。
	WarnLevel float64 `json:"warn_level"`
	// GuaranteeLevel 是保证水位，单位米。
	GuaranteeLevel float64 `json:"guarantee_level"`
	// HistoricLevel 是历史最高水位，单位米。
	HistoricLevel float64 `json:"historic_level"`
	// Rating 是水位流量关系曲线，未建站时为 nil。
	Rating *RatingCurve `json:"rating,omitempty"`
	Online bool         `json:"online"`
}

// Validate 校验水位站登记信息。
func (s Station) Validate() error {
	if strings.TrimSpace(s.Code) == "" {
		return fmt.Errorf("%w: 站码为空", ErrInvalidStation)
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("%w: 站点 %s 缺少站名", ErrInvalidStation, s.Code)
	}
	if _, err := ParseBasin(string(s.Basin)); err != nil {
		return fmt.Errorf("%w: 站点 %s 流域非法", ErrInvalidStation, s.Code)
	}
	if s.WarnLevel <= 0 {
		return fmt.Errorf("%w: 站点 %s 警戒水位必须为正", ErrInvalidStation, s.Code)
	}
	if s.GuaranteeLevel <= s.WarnLevel {
		return fmt.Errorf("%w: 站点 %s 保证水位必须高于警戒水位", ErrInvalidStation, s.Code)
	}
	if s.HistoricLevel < s.GuaranteeLevel {
		return fmt.Errorf("%w: 站点 %s 历史最高水位不应低于保证水位", ErrInvalidStation, s.Code)
	}
	return nil
}

// HasRating 报告该站是否建有水位流量关系曲线。
func (s Station) HasRating() bool {
	return s.Rating != nil
}

// Reading 表示一条水位观测记录。
type Reading struct {
	StationCode string    `json:"station_code"`
	At          time.Time `json:"at"`
	// LevelM 是观测水位，单位米。
	LevelM float64 `json:"level_m"`
	// RainfallMM 是时段降雨量，单位毫米。
	RainfallMM float64 `json:"rainfall_mm"`
}

// Validate 校验观测记录。
func (r Reading) Validate() error {
	if strings.TrimSpace(r.StationCode) == "" {
		return fmt.Errorf("%w: 缺少站码", ErrInvalidReading)
	}
	if r.At.IsZero() {
		return fmt.Errorf("%w: 站点 %s 缺少观测时刻", ErrInvalidReading, r.StationCode)
	}
	if r.RainfallMM < 0 {
		return fmt.Errorf("%w: 站点 %s 降雨量不得为负", ErrInvalidReading, r.StationCode)
	}
	return nil
}

// SortReadings 按观测时刻、站码排序，保证输出稳定。
func SortReadings(items []Reading) {
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].At.Equal(items[j].At) {
			return items[i].At.Before(items[j].At)
		}
		return items[i].StationCode < items[j].StationCode
	})
}

// Reach 表示一个河段。
type Reach struct {
	Code string `json:"code"`
	Name string `json:"name"`
	// UpstreamCode 是上断面站码。
	UpstreamCode string `json:"upstream_code"`
	// DownstreamCode 是下断面站码。
	DownstreamCode string `json:"downstream_code"`
	Basin          Basin  `json:"basin"`
	// LengthKM 是河段长度，单位公里。
	LengthKM float64 `json:"length_km"`
	// LeveeGrade 是堤防等级，1 为最高。
	LeveeGrade int `json:"levee_grade"`
}

// BreachState 表示溃口抢险状态。
type BreachState string

const (
	// BreachReported 已上报。
	BreachReported BreachState = "reported"
	// BreachSurveyed 已勘察。
	BreachSurveyed BreachState = "surveyed"
	// BreachClosing 正在合龙。
	BreachClosing BreachState = "closing"
	// BreachClosed 合龙成功。
	BreachClosed BreachState = "closed"
	// BreachAbandoned 已放弃封堵。
	BreachAbandoned BreachState = "abandoned"
)

// DisplayName 返回溃口状态中文名。
func (b BreachState) DisplayName() string {
	switch b {
	case BreachReported:
		return "已上报"
	case BreachSurveyed:
		return "已勘察"
	case BreachClosing:
		return "正在合龙"
	case BreachClosed:
		return "合龙成功"
	case BreachAbandoned:
		return "已放弃封堵"
	default:
		return string(b)
	}
}

// Terminal 报告该状态是否为终态。
func (b BreachState) Terminal() bool {
	return b == BreachClosed || b == BreachAbandoned
}

// Breach 表示一处溃口。
type Breach struct {
	ID         string      `json:"id"`
	ReachCode  string      `json:"reach_code"`
	Location   string      `json:"location"`
	WidthM     float64     `json:"width_m"`
	State      BreachState `json:"state"`
	ReportedAt time.Time   `json:"reported_at"`
	ClosedAt   time.Time   `json:"closed_at,omitempty"`
}

// Duration 返回从上报到合龙的历时。
func (b Breach) Duration() time.Duration {
	if b.ReportedAt.IsZero() || b.ClosedAt.IsZero() {
		return 0
	}
	return b.ClosedAt.Sub(b.ReportedAt)
}

// Crew 表示一支抢险队伍。
type Crew struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Base      string `json:"base"`
	Headcount int    `json:"headcount"`
	// CapacityM 是单日可封堵宽度，单位米。
	CapacityM float64 `json:"capacity_m"`
	Standby   bool    `json:"standby"`
}

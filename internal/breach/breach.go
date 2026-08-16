// Package breach 实现溃口抢险的登记、状态流转与力量编排。
package breach

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"floodwatch/internal/gauge"
	"floodwatch/internal/model"
)

// 抢险编排的重试策略：仅对上游数据服务的临时故障重试。
const (
	// MaxAttempts 是编排时对临时故障的最大尝试次数。
	MaxAttempts = 3
	// RetryBackoff 是两次尝试之间的等待时长。
	RetryBackoff = 20 * time.Millisecond
)

// transition 描述一条合法的状态流转边。
type edge struct {
	from model.BreachState
	to   model.BreachState
}

var edges = map[edge]bool{
	{model.BreachReported, model.BreachSurveyed}:  true,
	{model.BreachReported, model.BreachAbandoned}: true,
	{model.BreachSurveyed, model.BreachClosing}:   true,
	{model.BreachSurveyed, model.BreachAbandoned}: true,
	{model.BreachClosing, model.BreachClosed}:     true,
	{model.BreachClosing, model.BreachAbandoned}:  true,
}

// Allowed 返回某状态允许流转到的目标状态。
func Allowed(from model.BreachState) []model.BreachState {
	all := []model.BreachState{
		model.BreachReported, model.BreachSurveyed, model.BreachClosing,
		model.BreachClosed, model.BreachAbandoned,
	}
	out := make([]model.BreachState, 0, 3)
	for _, to := range all {
		if edges[edge{from, to}] {
			out = append(out, to)
		}
	}
	return out
}

// Service 提供溃口抢险能力。
type Service struct {
	mu       sync.Mutex
	registry *gauge.Registry
	breaches map[string]model.Breach
	crews    map[string]model.Crew
	seq      int
}

// NewService 构造抢险服务。
func NewService(reg *gauge.Registry) *Service {
	return &Service{
		registry: reg,
		breaches: make(map[string]model.Breach),
		crews:    make(map[string]model.Crew),
	}
}

// AddCrew 登记抢险队伍。
func (s *Service) AddCrew(c model.Crew) error {
	if strings.TrimSpace(c.ID) == "" {
		return fmt.Errorf("breach: 队伍编号为空")
	}
	if c.Headcount <= 0 {
		return fmt.Errorf("breach: 队伍 %s 人数必须为正", c.ID)
	}
	if c.CapacityM <= 0 {
		return fmt.Errorf("breach: 队伍 %s 单日封堵能力必须为正", c.ID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.crews[c.ID] = c
	return nil
}

// Crew 返回抢险队伍。
func (s *Service) Crew(id string) (model.Crew, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.crews[id]
	if !ok {
		return model.Crew{}, fmt.Errorf("%w: %s", model.ErrCrewUnknown, id)
	}
	return c, nil
}

// Crews 返回全部队伍，按编号排序。
func (s *Service) Crews() []model.Crew {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Crew, 0, len(s.crews))
	for _, c := range s.crews {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Report 登记一处溃口。
func (s *Service) Report(reachCode, location string, widthM float64, at time.Time) (model.Breach, error) {
	if _, err := s.registry.Reach(reachCode); err != nil {
		return model.Breach{}, err
	}
	if widthM <= 0 {
		return model.Breach{}, fmt.Errorf("breach: 溃口宽度必须为正, 收到 %.2f", widthM)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	b := model.Breach{
		ID:         fmt.Sprintf("B-%04d", s.seq),
		ReachCode:  reachCode,
		Location:   location,
		WidthM:     widthM,
		State:      model.BreachReported,
		ReportedAt: at,
	}
	s.breaches[b.ID] = b
	return b, nil
}

// Get 返回溃口。
func (s *Service) Get(id string) (model.Breach, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.breaches[id]
	if !ok {
		return model.Breach{}, fmt.Errorf("%w: %s", model.ErrBreachUnknown, id)
	}
	return b, nil
}

// List 返回全部溃口，按编号排序。
func (s *Service) List() []model.Breach {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Breach, 0, len(s.breaches))
	for _, b := range s.breaches {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Advance 推进溃口状态。
func (s *Service) Advance(id string, to model.BreachState, at time.Time) (model.Breach, error) {
	b, err := s.Get(id)
	if err != nil {
		return model.Breach{}, err
	}
	if !edges[edge{b.State, to}] {
		return model.Breach{}, fmt.Errorf("%w: 溃口 %s 不能从 %s 流转到 %s",
			model.ErrStateConflict, id, b.State.DisplayName(), to.DisplayName())
	}
	next := b
	next.State = to
	if to == model.BreachClosed {
		next.ClosedAt = at
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.breaches[id] = next
	return next, nil
}

// Plan 是抢险力量编排结果。
type Plan struct {
	BreachID  string   `json:"breach_id"`
	ReachCode string   `json:"reach_code"`
	WidthM    float64  `json:"width_m"`
	CrewIDs   []string `json:"crew_ids"`
	// TotalCapacityM 是编入队伍的单日合计封堵能力。
	TotalCapacityM float64 `json:"total_capacity_m"`
	// EstimatedDays 是预计合龙天数。
	EstimatedDays float64 `json:"estimated_days"`
	Attempts      int     `json:"attempts"`
}

// upstreamProbe 是上游数据服务探测函数，便于注入临时故障。
type upstreamProbe func(reachCode string) error

// IsRetryable 报告错误是否属于可退避重试的上游临时故障。
//
// 上游临时故障对应哨兵错误 model.ErrUpstreamTimeout。
func IsRetryable(err error) bool {
	return errors.Is(err, model.ErrUpstreamTimeout)
}

// IsDeterministic 报告错误是否属于不应重试的确定性错误。
func IsDeterministic(err error) bool {
	return errors.Is(err, model.ErrReachUnknown) ||
		errors.Is(err, model.ErrBreachUnknown) ||
		errors.Is(err, model.ErrStateConflict)
}

// Compose 为溃口编排抢险力量。
//
// 编排前需要向上游数据服务核对河段信息。只有上游临时故障
// （model.ErrUpstreamTimeout）才允许退避重试；河段不存在、溃口不存在
// 这类确定性错误必须立刻返回，不得重试。
func (s *Service) Compose(id string, probe upstreamProbe) (Plan, error) {
	b, err := s.Get(id)
	if err != nil {
		return Plan{}, err
	}

	attempts := 0
	for {
		attempts++
		perr := probe(b.ReachCode)
		if perr == nil {
			break
		}
		// 只有上游临时故障才重试；其余错误立刻返回。
		if !IsRetryable(perr) {
			return Plan{}, perr
		}
		if attempts >= MaxAttempts {
			return Plan{}, fmt.Errorf("breach: 溃口 %s 编排失败，已尝试 %d 次: %w",
				id, attempts, perr)
		}
		time.Sleep(RetryBackoff)
	}

	crews := s.Crews()
	plan := Plan{BreachID: b.ID, ReachCode: b.ReachCode, WidthM: b.WidthM, Attempts: attempts}
	for _, c := range crews {
		if !c.Standby {
			continue
		}
		plan.CrewIDs = append(plan.CrewIDs, c.ID)
		plan.TotalCapacityM += c.CapacityM
		if plan.TotalCapacityM >= b.WidthM {
			break
		}
	}
	if plan.TotalCapacityM <= 0 {
		return Plan{}, fmt.Errorf("%w: 溃口 %s 无可用抢险队伍", model.ErrCapacityShort, id)
	}
	if plan.TotalCapacityM < b.WidthM {
		return Plan{}, fmt.Errorf("%w: 溃口 %s 宽 %.1f 米，可用能力仅 %.1f 米",
			model.ErrCapacityShort, id, b.WidthM, plan.TotalCapacityM)
	}
	plan.EstimatedDays = roundUpDays(b.WidthM / plan.TotalCapacityM)
	return plan, nil
}

func roundUpDays(v float64) float64 {
	if v <= 0 {
		return 0
	}
	scaled := float64(int64(v*10)) / 10
	if scaled < v {
		scaled += 0.1
	}
	return scaled
}

// Counts 返回按状态分组的溃口数量。
func (s *Service) Counts() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int)
	for _, b := range s.breaches {
		out[string(b.State)]++
	}
	return out
}

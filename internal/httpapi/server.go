// Package httpapi 提供防汛水情监测与溃口抢险调度平台的 HTTP 接口。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"floodwatch/internal/breach"
	"floodwatch/internal/gauge"
	"floodwatch/internal/model"
	"floodwatch/internal/reach"
	"floodwatch/internal/report"
	"floodwatch/internal/warning"
)

// ErrorCode 是对外暴露的机器可读错误码。
type ErrorCode string

const (
	// CodeStationUnknown 水位站不存在。
	CodeStationUnknown ErrorCode = "station_unknown"
	// CodeStationOffline 水位站离线。
	CodeStationOffline ErrorCode = "station_offline"
	// CodeReachUnknown 河段不存在。
	CodeReachUnknown ErrorCode = "reach_unknown"
	// CodeBreachUnknown 溃口不存在。
	CodeBreachUnknown ErrorCode = "breach_unknown"
	// CodeCrewUnknown 抢险队伍不存在。
	CodeCrewUnknown ErrorCode = "crew_unknown"
	// CodeNoRatingCurve 该站未建水位流量关系曲线。
	CodeNoRatingCurve ErrorCode = "no_rating_curve"
	// CodeNoReadings 缺少观测数据。
	CodeNoReadings ErrorCode = "no_readings"
	// CodeStateConflict 状态流转冲突。
	CodeStateConflict ErrorCode = "state_conflict"
	// CodeCapacityShort 抢险力量不足。
	CodeCapacityShort ErrorCode = "capacity_short"
	// CodeStagingFull 临时作业空间超限。
	CodeStagingFull ErrorCode = "staging_full"
	// CodeUpstreamTimeout 上游数据服务超时。
	CodeUpstreamTimeout ErrorCode = "upstream_timeout"
	// CodeBadRequest 请求参数非法。
	CodeBadRequest ErrorCode = "bad_request"
	// CodeUnavailable 请求被取消或超时。
	CodeUnavailable ErrorCode = "unavailable"
	// CodeInternal 未归类的内部错误。
	CodeInternal ErrorCode = "internal"
)

var errorMapping = []struct {
	sentinel error
	status   int
	code     ErrorCode
}{
	{model.ErrStationOffline, http.StatusConflict, CodeStationOffline},
	{model.ErrStationUnknown, http.StatusNotFound, CodeStationUnknown},
	{model.ErrReachUnknown, http.StatusNotFound, CodeReachUnknown},
	{model.ErrBreachUnknown, http.StatusNotFound, CodeBreachUnknown},
	{model.ErrCrewUnknown, http.StatusNotFound, CodeCrewUnknown},
	{model.ErrNoRatingCurve, http.StatusUnprocessableEntity, CodeNoRatingCurve},
	{model.ErrNoReadings, http.StatusUnprocessableEntity, CodeNoReadings},
	{model.ErrStateConflict, http.StatusConflict, CodeStateConflict},
	{model.ErrCrewUnavailable, http.StatusConflict, CodeCapacityShort},
	{model.ErrCapacityShort, http.StatusConflict, CodeCapacityShort},
	{model.ErrStagingFull, http.StatusInsufficientStorage, CodeStagingFull},
	{model.ErrUpstreamTimeout, http.StatusBadGateway, CodeUpstreamTimeout},
	{model.ErrInvalidStation, http.StatusBadRequest, CodeBadRequest},
	{model.ErrInvalidReading, http.StatusBadRequest, CodeBadRequest},
	{model.ErrUnknownBasin, http.StatusBadRequest, CodeBadRequest},
	{model.ErrUnknownLevel, http.StatusBadRequest, CodeBadRequest},
	{context.Canceled, http.StatusServiceUnavailable, CodeUnavailable},
	{context.DeadlineExceeded, http.StatusServiceUnavailable, CodeUnavailable},
}

// Classify 依据错误链把领域错误映射为 HTTP 状态码与错误码。
func Classify(err error) (int, ErrorCode) {
	if err == nil {
		return http.StatusOK, ""
	}
	for _, m := range errorMapping {
		if errors.Is(err, m.sentinel) {
			return m.status, m.code
		}
	}
	return http.StatusInternalServerError, CodeInternal
}

// Server 是平台 HTTP 服务。
type Server struct {
	registry *gauge.Registry
	breaches *breach.Service
	reports  *report.Builder
	levels   map[string]float64
	readings []model.Reading
	now      func() time.Time
}

// Options 是构造 Server 所需的依赖。
type Options struct {
	Registry *gauge.Registry
	Breaches *breach.Service
	Levels   map[string]float64
	Readings []model.Reading
	Now      func() time.Time
}

// New 构造 HTTP 服务。
func New(opts Options) *Server {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	levels := opts.Levels
	if levels == nil {
		levels = map[string]float64{}
	}
	return &Server{
		registry: opts.Registry,
		breaches: opts.Breaches,
		reports:  report.NewBuilder(opts.Registry),
		levels:   levels,
		readings: opts.Readings,
		now:      now,
	}
}

// Handler 返回注册好全部路由的处理器。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /api/stations", s.handleStations)
	mux.HandleFunc("GET /api/stations/{code}", s.handleStation)
	mux.HandleFunc("GET /api/stations/{code}/discharge", s.handleDischarge)
	mux.HandleFunc("GET /api/stations/{code}/thresholds", s.handleThresholds)
	mux.HandleFunc("GET /api/reaches", s.handleReaches)
	mux.HandleFunc("GET /api/series/analyse", s.handleAnalyse)
	mux.HandleFunc("GET /api/report/situation", s.handleSituation)
	mux.HandleFunc("GET /api/report/basins", s.handleBasins)
	mux.HandleFunc("GET /api/breaches", s.handleBreaches)
	mux.HandleFunc("GET /api/breaches/{id}", s.handleBreach)
	mux.HandleFunc("POST /api/breaches", s.handleReportBreach)
	mux.HandleFunc("POST /api/breaches/{id}/advance", s.handleAdvance)
	mux.HandleFunc("POST /api/breaches/{id}/compose", s.handleCompose)
	mux.HandleFunc("GET /api/crews", s.handleCrews)
	return mux
}

type errorBody struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(payload)
}

func (s *Server) writeError(w http.ResponseWriter, err error) {
	status, code := Classify(err)
	s.writeJSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: err.Error()}})
}

func (s *Server) badRequest(w http.ResponseWriter, msg string) {
	s.writeJSON(w, http.StatusBadRequest, errorEnvelope{Error: errorBody{Code: CodeBadRequest, Message: msg}})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	c := s.registry.Counts()
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"service":     "floodwatch",
		"stations":    c.Stations,
		"online":      c.Online,
		"with_rating": c.WithRating,
		"reaches":     c.Reaches,
	})
}

func (s *Server) handleStations(w http.ResponseWriter, r *http.Request) {
	if raw := strings.TrimSpace(r.URL.Query().Get("basin")); raw != "" {
		b, err := model.ParseBasin(raw)
		if err != nil {
			s.writeError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"stations": s.registry.StationsByBasin(b)})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"stations": s.registry.Stations()})
}

func (s *Server) handleStation(w http.ResponseWriter, r *http.Request) {
	st, err := s.registry.Station(r.PathValue("code"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	payload := map[string]any{"station": st, "has_rating": st.HasRating()}
	if levelM, ok := s.levels[st.Code]; ok {
		a, aerr := warning.Assess(levelM, st)
		if aerr != nil {
			s.writeError(w, aerr)
			return
		}
		payload["assessment"] = a
	}
	s.writeJSON(w, http.StatusOK, payload)
}

// handleDischarge 推算流量。未建曲线的站点返回 422 与 no_rating_curve。
func (s *Server) handleDischarge(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	st, err := s.registry.Station(code)
	if err != nil {
		s.writeError(w, err)
		return
	}
	levelM, ok := s.levels[code]
	if raw := strings.TrimSpace(r.URL.Query().Get("level_m")); raw != "" {
		v, perr := strconv.ParseFloat(raw, 64)
		if perr != nil {
			s.badRequest(w, "httpapi: level_m 需为数字")
			return
		}
		levelM, ok = v, true
	}
	if !ok {
		s.badRequest(w, "httpapi: 缺少 level_m 且该站无当前水位")
		return
	}

	q, derr := gauge.Discharge(st, levelM)
	if derr != nil {
		s.writeError(w, derr)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"station_code":  code,
		"level_m":       levelM,
		"discharge_m3s": q,
		"has_rating":    st.HasRating(),
	})
}

func (s *Server) handleThresholds(w http.ResponseWriter, r *http.Request) {
	st, err := s.registry.Station(r.PathValue("code"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	items, terr := warning.Thresholds(st)
	if terr != nil {
		s.writeError(w, terr)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"station_code": st.Code, "thresholds": items})
}

func (s *Server) handleReaches(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"reaches": s.registry.Reaches()})
}

// handleAnalyse 对内置观测序列做只读分析。
func (s *Server) handleAnalyse(w http.ResponseWriter, r *http.Request) {
	window := 3
	if raw := strings.TrimSpace(r.URL.Query().Get("window")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			s.badRequest(w, "httpapi: window 需为正整数")
			return
		}
		window = n
	}
	if len(s.readings) == 0 {
		s.writeError(w, model.ErrNoReadings)
		return
	}
	// 分析是只读操作，同一份序列连续分析两次结果必须一致。
	first, err := reach.Analyse(s.readings, window)
	if err != nil {
		s.writeError(w, err)
		return
	}
	second, err := reach.Analyse(s.readings, window)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"first":         first,
		"second":        second,
		"stable":        first.PeakM == second.PeakM && first.Rate == second.Rate,
		"chronological": reach.Chronological(s.readings),
	})
}

func (s *Server) handleSituation(w http.ResponseWriter, r *http.Request) {
	sit, err := s.reports.Situation(s.levels)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, sit)
}

func (s *Server) handleBasins(w http.ResponseWriter, r *http.Request) {
	lines, err := s.reports.Basins(s.levels)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"basins": lines})
}

func (s *Server) handleBreaches(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{
		"breaches": s.breaches.List(),
		"counts":   s.breaches.Counts(),
	})
}

func (s *Server) handleBreach(w http.ResponseWriter, r *http.Request) {
	b, err := s.breaches.Get(r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"breach":       b,
		"state_name":   b.State.DisplayName(),
		"allowed_next": breach.Allowed(b.State),
	})
}

type reportBreachRequest struct {
	ReachCode string  `json:"reach_code"`
	Location  string  `json:"location"`
	WidthM    float64 `json:"width_m"`
}

func (s *Server) handleReportBreach(w http.ResponseWriter, r *http.Request) {
	var body reportBreachRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.badRequest(w, "httpapi: 请求体不是合法 JSON")
		return
	}
	b, err := s.breaches.Report(body.ReachCode, body.Location, body.WidthM, s.now())
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, b)
}

type advanceRequest struct {
	To string `json:"to"`
}

func (s *Server) handleAdvance(w http.ResponseWriter, r *http.Request) {
	var body advanceRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.badRequest(w, "httpapi: 请求体不是合法 JSON")
		return
	}
	next, err := s.breaches.Advance(r.PathValue("id"), model.BreachState(strings.TrimSpace(body.To)), s.now())
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, next)
}

// handleCompose 编排抢险力量。
// upstream 查询参数用于演练上游状态：ok / unknown-reach / timeout。
func (s *Server) handleCompose(w http.ResponseWriter, r *http.Request) {
	mode := strings.TrimSpace(r.URL.Query().Get("upstream"))
	if mode == "" {
		mode = "ok"
	}
	probe, err := probeFor(mode)
	if err != nil {
		s.badRequest(w, err.Error())
		return
	}

	begin := time.Now()
	plan, cerr := s.breaches.Compose(r.PathValue("id"), probe)
	elapsed := time.Since(begin)
	if cerr != nil {
		status, code := Classify(cerr)
		s.writeJSON(w, status, map[string]any{
			"error":      errorBody{Code: code, Message: cerr.Error()},
			"elapsed_ms": elapsed.Milliseconds(),
			"upstream":   mode,
		})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"plan":       plan,
		"elapsed_ms": elapsed.Milliseconds(),
		"upstream":   mode,
	})
}

func (s *Server) handleCrews(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"crews": s.breaches.Crews()})
}

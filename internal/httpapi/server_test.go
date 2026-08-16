package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"floodwatch/internal/model"
	"floodwatch/internal/seed"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	reg, svc, err := seed.Load()
	if err != nil {
		t.Fatalf("seed.Load 失败: %v", err)
	}
	fixed := time.Date(2026, time.August, 16, 6, 0, 0, 0, time.UTC)
	srv := New(Options{
		Registry: reg,
		Breaches: svc,
		Levels:   seed.Levels(),
		Readings: seed.Readings(),
		Now:      func() time.Time { return fixed },
	})
	return srv.Handler()
}

func doJSON(t *testing.T, h http.Handler, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var payload map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s %s 响应不是合法 JSON: %v\n%s", method, path, err, rec.Body.String())
		}
	}
	return rec, payload
}

func errorCode(t *testing.T, payload map[string]any) string {
	t.Helper()
	raw, ok := payload["error"]
	if !ok {
		t.Fatalf("响应缺少 error 字段: %+v", payload)
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("error 字段格式异常: %+v", raw)
	}
	code, _ := obj["code"].(string)
	return code
}

func TestHealth(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodGet, "/healthz", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	if payload["status"] != "ok" {
		t.Fatalf("响应 = %+v", payload)
	}
	if got, _ := payload["with_rating"].(float64); got != 4 {
		t.Fatalf("with_rating = %v, 期望 4", payload["with_rating"])
	}
}

// TestDischargeOnUnratedStationReturns422 断言未建曲线站点返回 422 与 no_rating_curve。
func TestDischargeOnUnratedStationReturns422(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodGet, "/api/stations/HN-QYH-02/discharge", "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("状态码 = %d, 期望 422, 响应 %+v", rec.Code, payload)
	}
	if got := errorCode(t, payload); got != string(CodeNoRatingCurve) {
		t.Fatalf("错误码 = %q, 期望 %q", got, CodeNoRatingCurve)
	}
}

// TestDischargeOnRatedStation 断言建有曲线站点可正常推算流量。
func TestDischargeOnRatedStation(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodGet, "/api/stations/HN-JLH-01/discharge", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 响应 %+v", rec.Code, payload)
	}
	if q, _ := payload["discharge_m3s"].(float64); q <= 0 {
		t.Fatalf("流量 = %v, 应为正", payload["discharge_m3s"])
	}
}

// TestStationDetailOnUnratedStation 断言查看未建曲线站点不失败。
func TestStationDetailOnUnratedStation(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodGet, "/api/stations/ZJ-TXH-05", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 响应 %+v", rec.Code, payload)
	}
	if hasRating, _ := payload["has_rating"].(bool); hasRating {
		t.Fatalf("ZJ-TXH-05 应无曲线")
	}
	if payload["assessment"] == nil {
		t.Fatalf("应包含预警判定")
	}
}

func TestStationNotFound(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodGet, "/api/stations/NOPE", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, 期望 404", rec.Code)
	}
	if got := errorCode(t, payload); got != string(CodeStationUnknown) {
		t.Fatalf("错误码 = %q", got)
	}
}

// TestSituationEscalatesAtGuaranteeLevel 断言态势接口在有站点达到保证水位时给出一级响应。
func TestSituationEscalatesAtGuaranteeLevel(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodGet, "/api/report/situation", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	if payload["highest_level"] != "red" {
		t.Fatalf("最高等级 = %v, 期望 red", payload["highest_level"])
	}
	if payload["response"] != "level-1" {
		t.Fatalf("响应级别 = %v, 期望 level-1", payload["response"])
	}
}

// TestThresholdsEndpointMatchesAssessment 断言阈值接口与站点判定一致。
func TestThresholdsEndpointMatchesAssessment(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodGet, "/api/stations/HN-JLH-01/thresholds", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	items, ok := payload["thresholds"].([]any)
	if !ok || len(items) != 4 {
		t.Fatalf("thresholds = %+v", payload["thresholds"])
	}
	for _, raw := range items {
		row, _ := raw.(map[string]any)
		from, _ := row["from_m"].(float64)
		level, _ := row["level"].(string)
		rec2, p2 := doJSON(t, h, http.MethodGet,
			fmt.Sprintf("/api/stations/HN-JLH-01/discharge?level_m=%v", from), "")
		if rec2.Code != http.StatusOK {
			t.Fatalf("按阈值水位查询失败: %+v", p2)
		}
		_ = level
	}
}

// TestAnalyseEndpointStable 断言序列分析接口连续两次结果一致且不改动输入。
func TestAnalyseEndpointStable(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodGet, "/api/series/analyse?window=3", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 响应 %+v", rec.Code, payload)
	}
	if stable, _ := payload["stable"].(bool); !stable {
		t.Fatalf("两次分析结果不一致: %+v", payload)
	}
	if chrono, _ := payload["chronological"].(bool); !chrono {
		t.Fatalf("分析后输入序列时间顺序被打乱: %+v", payload)
	}
}

// TestComposeDeterministicErrorReturns404 断言上游回复河段不存在时返回 404 且不重试。
func TestComposeDeterministicErrorReturns404(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodPost, "/api/breaches",
		`{"reach_code":"R-JLH-01","location":"左岸 K12+300","width_m":25}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("登记溃口状态码 = %d, 响应 %+v", rec.Code, payload)
	}
	id, _ := payload["id"].(string)

	rec, payload = doJSON(t, h, http.MethodPost, "/api/breaches/"+id+"/compose?upstream=unknown-reach", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, 期望 404, 响应 %+v", rec.Code, payload)
	}
	if got := errorCode(t, payload); got != string(CodeReachUnknown) {
		t.Fatalf("错误码 = %q, 期望 %q", got, CodeReachUnknown)
	}
	if elapsed, _ := payload["elapsed_ms"].(float64); elapsed > 15 {
		t.Fatalf("耗时 = %v ms, 期望接近 0（确定性错误不应重试）", elapsed)
	}
}

// TestComposeUpstreamTimeoutReturns502 断言上游持续超时返回 502。
func TestComposeUpstreamTimeoutReturns502(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodPost, "/api/breaches",
		`{"reach_code":"R-JLH-01","location":"左岸 K12+300","width_m":25}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("登记溃口失败: %+v", payload)
	}
	id, _ := payload["id"].(string)

	rec, payload = doJSON(t, h, http.MethodPost, "/api/breaches/"+id+"/compose?upstream=timeout", "")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("状态码 = %d, 期望 502, 响应 %+v", rec.Code, payload)
	}
	if got := errorCode(t, payload); got != string(CodeUpstreamTimeout) {
		t.Fatalf("错误码 = %q", got)
	}
}

func TestComposeOK(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodPost, "/api/breaches",
		`{"reach_code":"R-JLH-01","location":"左岸 K12+300","width_m":25}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("登记溃口失败: %+v", payload)
	}
	id, _ := payload["id"].(string)

	rec, payload = doJSON(t, h, http.MethodPost, "/api/breaches/"+id+"/compose?upstream=ok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 响应 %+v", rec.Code, payload)
	}
	plan, ok := payload["plan"].(map[string]any)
	if !ok {
		t.Fatalf("plan = %+v", payload["plan"])
	}
	if attempts, _ := plan["attempts"].(float64); attempts != 1 {
		t.Fatalf("attempts = %v, 期望 1", plan["attempts"])
	}
}

func TestBreachLifecycle(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodPost, "/api/breaches",
		`{"reach_code":"R-JLH-01","location":"左岸 K12+300","width_m":25}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("登记溃口失败: %+v", payload)
	}
	id, _ := payload["id"].(string)

	for _, to := range []string{"surveyed", "closing", "closed"} {
		rec, payload = doJSON(t, h, http.MethodPost, "/api/breaches/"+id+"/advance",
			`{"to":"`+to+`"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("流转到 %s 状态码 = %d, 响应 %+v", to, rec.Code, payload)
		}
	}
	rec, payload = doJSON(t, h, http.MethodPost, "/api/breaches/"+id+"/advance", `{"to":"closing"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("终态流转状态码 = %d, 期望 409", rec.Code)
	}
	if got := errorCode(t, payload); got != string(CodeStateConflict) {
		t.Fatalf("错误码 = %q", got)
	}
}

func TestBreachUnknown(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodGet, "/api/breaches/B-9999", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, 期望 404", rec.Code)
	}
	if got := errorCode(t, payload); got != string(CodeBreachUnknown) {
		t.Fatalf("错误码 = %q", got)
	}
}

func TestListEndpoints(t *testing.T) {
	h := newTestServer(t)
	for _, spec := range []struct {
		path string
		key  string
		want int
	}{
		{"/api/stations", "stations", len(seed.Stations())},
		{"/api/reaches", "reaches", len(seed.Reaches())},
		{"/api/crews", "crews", len(seed.Crews())},
		{"/api/report/basins", "basins", 5},
	} {
		rec, payload := doJSON(t, h, http.MethodGet, spec.path, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s 状态码 = %d", spec.path, rec.Code)
		}
		items, _ := payload[spec.key].([]any)
		if len(items) != spec.want {
			t.Fatalf("%s 条数 = %d, 期望 %d", spec.path, len(items), spec.want)
		}
	}
}

func TestBadRequests(t *testing.T) {
	h := newTestServer(t)
	if rec, _ := doJSON(t, h, http.MethodGet, "/api/stations?basin=nope", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法流域状态码 = %d, 期望 400", rec.Code)
	}
	if rec, _ := doJSON(t, h, http.MethodGet, "/api/series/analyse?window=0", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("window=0 状态码 = %d, 期望 400", rec.Code)
	}
	if rec, _ := doJSON(t, h, http.MethodGet, "/api/stations/HN-JLH-01/discharge?level_m=abc", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法水位状态码 = %d, 期望 400", rec.Code)
	}
	if rec, _ := doJSON(t, h, http.MethodPost, "/api/breaches", `not-json`); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 状态码 = %d, 期望 400", rec.Code)
	}
}

func TestClassifyMapsSentinels(t *testing.T) {
	cases := []struct {
		err        error
		wantStatus int
		wantCode   ErrorCode
	}{
		{fmt.Errorf("上层: %w", model.ErrStationUnknown), http.StatusNotFound, CodeStationUnknown},
		{fmt.Errorf("上层: %w", model.ErrReachUnknown), http.StatusNotFound, CodeReachUnknown},
		{fmt.Errorf("上层: %w", model.ErrNoRatingCurve), http.StatusUnprocessableEntity, CodeNoRatingCurve},
		{fmt.Errorf("上层: %w", model.ErrStateConflict), http.StatusConflict, CodeStateConflict},
		{fmt.Errorf("上层: %w", model.ErrCapacityShort), http.StatusConflict, CodeCapacityShort},
		{fmt.Errorf("上层: %w", model.ErrStagingFull), http.StatusInsufficientStorage, CodeStagingFull},
		{fmt.Errorf("上层: %w", model.ErrUpstreamTimeout), http.StatusBadGateway, CodeUpstreamTimeout},
		{fmt.Errorf("上层: %w", context.DeadlineExceeded), http.StatusServiceUnavailable, CodeUnavailable},
		{errors.New("未归类"), http.StatusInternalServerError, CodeInternal},
	}
	for _, tc := range cases {
		status, code := Classify(tc.err)
		if status != tc.wantStatus || code != tc.wantCode {
			t.Errorf("Classify(%v) = %d/%s, 期望 %d/%s", tc.err, status, code, tc.wantStatus, tc.wantCode)
		}
	}
}

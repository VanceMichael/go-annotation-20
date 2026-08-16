package breach

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"floodwatch/internal/gauge"
	"floodwatch/internal/model"
)

func at(h int) time.Time {
	return time.Date(2026, time.August, 16, h, 0, 0, 0, time.UTC)
}

func fixture(t *testing.T) *Service {
	t.Helper()
	reg := gauge.New()
	up := model.Station{
		Code: "HN-JLH-01", Name: "贾鲁河鄢陵站", River: "贾鲁河", Basin: model.BasinHuaihe,
		WarnLevel: 22.0, GuaranteeLevel: 25.0, HistoricLevel: 26.5, Online: true,
	}
	down := model.Station{
		Code: "HN-QYH-02", Name: "清潩河仓头站", River: "清潩河", Basin: model.BasinHuaihe,
		WarnLevel: 19.5, GuaranteeLevel: 21.8, HistoricLevel: 22.4, Online: true,
	}
	for _, s := range []model.Station{up, down} {
		if err := reg.AddStation(s); err != nil {
			t.Fatalf("AddStation 失败: %v", err)
		}
	}
	if err := reg.AddReach(model.Reach{
		Code: "R-01", Name: "贾鲁河中段", UpstreamCode: up.Code, DownstreamCode: down.Code,
		Basin: model.BasinHuaihe, LengthKM: 42.5, LeveeGrade: 2,
	}); err != nil {
		t.Fatalf("AddReach 失败: %v", err)
	}

	svc := NewService(reg)
	crews := []model.Crew{
		{ID: "C-01", Name: "省级机动抢险队", Base: "郑州", Headcount: 120, CapacityM: 18, Standby: true},
		{ID: "C-02", Name: "市级抢险队", Base: "许昌", Headcount: 60, CapacityM: 10, Standby: true},
		{ID: "C-03", Name: "县级抢险队", Base: "鄢陵", Headcount: 40, CapacityM: 6, Standby: true},
	}
	for _, c := range crews {
		if err := svc.AddCrew(c); err != nil {
			t.Fatalf("AddCrew 失败: %v", err)
		}
	}
	return svc
}

// okProbe 是始终成功的上游探测。
func okProbe(string) error { return nil }

// unknownReachProbe 模拟上游明确回复「河段不存在」这类确定性错误。
func unknownReachProbe(code string) error {
	return fmt.Errorf("upstream: 核对河段 %s 失败: %w", code, model.ErrReachUnknown)
}

// TestComposeDoesNotRetryDeterministicError 断言确定性错误立刻返回，不进入重试。
func TestComposeDoesNotRetryDeterministicError(t *testing.T) {
	svc := fixture(t)
	b, err := svc.Report("R-01", "左岸 K12+300", 25, at(6))
	if err != nil {
		t.Fatalf("Report 失败: %v", err)
	}

	calls := 0
	probe := func(code string) error {
		calls++
		return unknownReachProbe(code)
	}

	begin := time.Now()
	_, cerr := svc.Compose(b.ID, probe)
	elapsed := time.Since(begin)

	if cerr == nil {
		t.Fatalf("上游回复河段不存在时应返回错误")
	}
	if !errors.Is(cerr, model.ErrReachUnknown) {
		t.Fatalf("errors.Is(err, model.ErrReachUnknown) = false, 错误为 %v", cerr)
	}
	if errors.Is(cerr, model.ErrUpstreamTimeout) {
		t.Fatalf("确定性错误不应被归类为上游超时: %v", cerr)
	}
	if calls != 1 {
		t.Fatalf("上游探测被调用 %d 次, 期望 1 次（确定性错误不得重试）", calls)
	}
	if elapsed >= RetryBackoff {
		t.Fatalf("耗时 %v, 期望不发生退避等待（单次退避 %v）", elapsed, RetryBackoff)
	}
}

// TestComposeRetriesOnlyUpstreamTimeout 断言仅上游临时故障才退避重试。
func TestComposeRetriesOnlyUpstreamTimeout(t *testing.T) {
	svc := fixture(t)
	b, err := svc.Report("R-01", "左岸 K12+300", 25, at(6))
	if err != nil {
		t.Fatalf("Report 失败: %v", err)
	}

	calls := 0
	probe := func(string) error {
		calls++
		if calls < 2 {
			return fmt.Errorf("upstream: 网关无响应: %w", model.ErrUpstreamTimeout)
		}
		return nil
	}

	plan, cerr := svc.Compose(b.ID, probe)
	if cerr != nil {
		t.Fatalf("临时故障恢复后应成功: %v", cerr)
	}
	if calls != 2 {
		t.Fatalf("上游探测被调用 %d 次, 期望 2 次", calls)
	}
	if plan.Attempts != 2 {
		t.Fatalf("Attempts = %d, 期望 2", plan.Attempts)
	}
}

// TestIsRetryableUsesErrorChain 断言可重试判定基于错误链，包装后仍能识别。
func TestIsRetryableUsesErrorChain(t *testing.T) {
	bare := model.ErrUpstreamTimeout
	wrapped := fmt.Errorf("upstream: 网关无响应: %w", model.ErrUpstreamTimeout)
	deep := fmt.Errorf("compose: %w", wrapped)

	for name, err := range map[string]error{"裸哨兵": bare, "一层包装": wrapped, "两层包装": deep} {
		if !IsRetryable(err) {
			t.Errorf("%s 应判定为可重试: %v", name, err)
		}
	}
	for name, err := range map[string]error{
		"河段不存在": fmt.Errorf("upstream: %w", model.ErrReachUnknown),
		"状态冲突":  fmt.Errorf("x: %w", model.ErrStateConflict),
		"nil":   nil,
	} {
		if IsRetryable(err) {
			t.Errorf("%s 不应判定为可重试: %v", name, err)
		}
	}
}

// TestIsDeterministicUsesErrorChain 断言确定性错误判定基于错误链。
func TestIsDeterministicUsesErrorChain(t *testing.T) {
	if !IsDeterministic(fmt.Errorf("upstream: %w", model.ErrReachUnknown)) {
		t.Errorf("包装后的河段不存在应判定为确定性错误")
	}
	if IsDeterministic(fmt.Errorf("upstream: %w", model.ErrUpstreamTimeout)) {
		t.Errorf("上游超时不应判定为确定性错误")
	}
}

// TestComposeRecoversAfterTransientFailures 断言上游前两次超时、第三次恢复时编排最终成功。
func TestComposeRecoversAfterTransientFailures(t *testing.T) {
	svc := fixture(t)
	b, err := svc.Report("R-01", "左岸 K12+300", 25, at(6))
	if err != nil {
		t.Fatalf("Report 失败: %v", err)
	}

	calls := 0
	probe := func(string) error {
		calls++
		if calls < 3 {
			return fmt.Errorf("upstream: 网关第 %d 次无响应: %w", calls, model.ErrUpstreamTimeout)
		}
		return nil
	}

	plan, cerr := svc.Compose(b.ID, probe)
	if cerr != nil {
		t.Fatalf("上游第三次恢复后编排应成功, 实际 %v（上游被调用 %d 次）", cerr, calls)
	}
	if calls != 3 {
		t.Fatalf("上游被调用 %d 次, 期望 3 次（前两次超时需退避重试）", calls)
	}
	if plan.Attempts != 3 {
		t.Fatalf("Attempts = %d, 期望 3", plan.Attempts)
	}
	if len(plan.CrewIDs) == 0 {
		t.Fatalf("编排结果为空: %+v", plan)
	}
}

// TestComposeGivesUpAfterMaxAttempts 断言持续超时时在上限后放弃并返回超时错误。
func TestComposeGivesUpAfterMaxAttempts(t *testing.T) {
	svc := fixture(t)
	b, err := svc.Report("R-01", "左岸 K12+300", 25, at(6))
	if err != nil {
		t.Fatalf("Report 失败: %v", err)
	}

	calls := 0
	probe := func(string) error {
		calls++
		return fmt.Errorf("upstream: 网关无响应: %w", model.ErrUpstreamTimeout)
	}

	_, cerr := svc.Compose(b.ID, probe)
	if !errors.Is(cerr, model.ErrUpstreamTimeout) {
		t.Fatalf("持续超时应返回 ErrUpstreamTimeout, 实际 %v", cerr)
	}
	if calls != MaxAttempts {
		t.Fatalf("上游探测被调用 %d 次, 期望 %d 次", calls, MaxAttempts)
	}
}

// TestComposeUnknownBreachReturnsImmediately 断言溃口不存在时立刻返回，不调用上游。
func TestComposeUnknownBreachReturnsImmediately(t *testing.T) {
	svc := fixture(t)
	calls := 0
	probe := func(string) error {
		calls++
		return nil
	}
	_, err := svc.Compose("B-9999", probe)
	if !errors.Is(err, model.ErrBreachUnknown) {
		t.Fatalf("未知溃口应返回 ErrBreachUnknown, 实际 %v", err)
	}
	if calls != 0 {
		t.Fatalf("未知溃口不应调用上游, 实际调用 %d 次", calls)
	}
}

func TestComposeSuccess(t *testing.T) {
	svc := fixture(t)
	b, err := svc.Report("R-01", "左岸 K12+300", 25, at(6))
	if err != nil {
		t.Fatalf("Report 失败: %v", err)
	}
	plan, err := svc.Compose(b.ID, okProbe)
	if err != nil {
		t.Fatalf("Compose 失败: %v", err)
	}
	if len(plan.CrewIDs) < 2 {
		t.Fatalf("编入队伍 = %v, 宽 25 米应至少需要 2 支", plan.CrewIDs)
	}
	if plan.TotalCapacityM < plan.WidthM {
		t.Fatalf("合计能力 %.1f 小于溃口宽度 %.1f", plan.TotalCapacityM, plan.WidthM)
	}
	if plan.EstimatedDays <= 0 {
		t.Fatalf("预计合龙天数 = %.2f", plan.EstimatedDays)
	}
	if plan.Attempts != 1 {
		t.Fatalf("Attempts = %d, 期望 1", plan.Attempts)
	}
}

func TestComposeCapacityShort(t *testing.T) {
	svc := fixture(t)
	b, err := svc.Report("R-01", "左岸 K18+100", 200, at(6))
	if err != nil {
		t.Fatalf("Report 失败: %v", err)
	}
	if _, cerr := svc.Compose(b.ID, okProbe); !errors.Is(cerr, model.ErrCapacityShort) {
		t.Fatalf("能力不足应返回 ErrCapacityShort, 实际 %v", cerr)
	}
}

func TestReportAndAdvance(t *testing.T) {
	svc := fixture(t)
	if _, err := svc.Report("NOPE", "x", 10, at(6)); !errors.Is(err, model.ErrReachUnknown) {
		t.Fatalf("未知河段应返回 ErrReachUnknown, 实际 %v", err)
	}
	if _, err := svc.Report("R-01", "x", 0, at(6)); err == nil {
		t.Fatalf("宽度为 0 应返回错误")
	}

	b, err := svc.Report("R-01", "左岸 K12+300", 25, at(6))
	if err != nil {
		t.Fatalf("Report 失败: %v", err)
	}
	for _, to := range []model.BreachState{model.BreachSurveyed, model.BreachClosing, model.BreachClosed} {
		b, err = svc.Advance(b.ID, to, at(20))
		if err != nil {
			t.Fatalf("Advance %s 失败: %v", to, err)
		}
	}
	if b.State != model.BreachClosed {
		t.Fatalf("最终状态 = %s", b.State)
	}
	if b.Duration() != 14*time.Hour {
		t.Fatalf("历时 = %v, 期望 14h", b.Duration())
	}
	if _, err := svc.Advance(b.ID, model.BreachClosing, at(21)); !errors.Is(err, model.ErrStateConflict) {
		t.Fatalf("终态流转应返回 ErrStateConflict, 实际 %v", err)
	}
}

func TestAllowedStates(t *testing.T) {
	if got := Allowed(model.BreachReported); len(got) != 2 {
		t.Fatalf("已上报允许流转 = %v", got)
	}
	if got := Allowed(model.BreachClosed); len(got) != 0 {
		t.Fatalf("合龙成功不应允许再流转, 实际 %v", got)
	}
}

func TestCrewRegistration(t *testing.T) {
	svc := fixture(t)
	if _, err := svc.Crew("NOPE"); !errors.Is(err, model.ErrCrewUnknown) {
		t.Fatalf("未知队伍应返回 ErrCrewUnknown, 实际 %v", err)
	}
	if err := svc.AddCrew(model.Crew{ID: "", Headcount: 1, CapacityM: 1}); err == nil {
		t.Errorf("空编号应返回错误")
	}
	if err := svc.AddCrew(model.Crew{ID: "X", Headcount: 0, CapacityM: 1}); err == nil {
		t.Errorf("人数为 0 应返回错误")
	}
	if err := svc.AddCrew(model.Crew{ID: "X", Headcount: 1, CapacityM: 0}); err == nil {
		t.Errorf("能力为 0 应返回错误")
	}
	if got := len(svc.Crews()); got != 3 {
		t.Fatalf("队伍数 = %d, 期望 3", got)
	}
}

// writeShard 在 dir 下写一个分片文件，padding 用于放大文件体积。
func writeShard(t *testing.T, dir string, idx int, records int, padding int) string {
	t.Helper()
	sh := Shard{ReachCode: "R-01"}
	for i := 0; i < records; i++ {
		sh.Records = append(sh.Records, ShardRecord{
			Location: fmt.Sprintf("左岸 K%02d+%03d", idx, i*100),
			WidthM:   float64(8 + i),
			Source:   "巡堤查险" + string(make([]byte, 0)),
		})
	}
	data, err := json.Marshal(sh)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	// 用 JSON 之外的填充撑大文件体积，模拟真实分片附带的原始报文。
	padded := append(data, make([]byte, padding)...)
	path := filepath.Join(dir, fmt.Sprintf("shard-%02d.json", idx))
	if werr := os.WriteFile(path, padded, 0o644); werr != nil {
		t.Fatalf("写分片失败: %v", werr)
	}
	return path
}

// TestImportShardsReleasesStagingPerShard 断言逐个分片导入时作业目录占用不累积。
func TestImportShardsReleasesStagingPerShard(t *testing.T) {
	svc := fixture(t)
	srcDir := t.TempDir()
	stagingDir := filepath.Join(t.TempDir(), "staging")

	// 8 个分片，每个约 3KB；上限 8KB。逐个释放时峰值应约等于单片大小。
	var paths []string
	for i := 1; i <= 8; i++ {
		paths = append(paths, writeShard(t, srcDir, i, 2, 3000))
	}

	im := NewImporter(svc, 8*1024)
	rep, err := im.ImportShards(paths, stagingDir, func() time.Time { return at(6) })
	if err != nil {
		t.Fatalf("导入 8 个分片失败: %v（峰值占用 %d 字节，上限 %d 字节）",
			err, rep.PeakStagingBytes, im.StagingLimitBytes)
	}
	if rep.Shards != 8 {
		t.Fatalf("导入分片数 = %d, 期望 8", rep.Shards)
	}
	if rep.Records != 16 {
		t.Fatalf("导入记录数 = %d, 期望 16", rep.Records)
	}
	if rep.PeakStagingBytes > 6*1024 {
		t.Fatalf("作业目录峰值占用 = %d 字节, 期望不超过单片规模（约 3KB）；占用随分片数累积说明临时副本未及时释放",
			rep.PeakStagingBytes)
	}
}

// TestImportShardsStagingDirEmptyAfterImport 断言导入结束后作业目录已清空。
func TestImportShardsStagingDirEmptyAfterImport(t *testing.T) {
	svc := fixture(t)
	srcDir := t.TempDir()
	stagingDir := filepath.Join(t.TempDir(), "staging")

	var paths []string
	for i := 1; i <= 5; i++ {
		paths = append(paths, writeShard(t, srcDir, i, 1, 1200))
	}

	im := NewImporter(svc, 32*1024)
	if _, err := im.ImportShards(paths, stagingDir, func() time.Time { return at(6) }); err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	used, err := stagingUsage(stagingDir)
	if err != nil {
		t.Fatalf("stagingUsage 失败: %v", err)
	}
	if used != 0 {
		t.Fatalf("导入结束后作业目录仍占用 %d 字节, 期望 0", used)
	}
}

// TestImportShardsManySmallShards 断言分片数量增加不会触发容量上限。
func TestImportShardsManySmallShards(t *testing.T) {
	svc := fixture(t)
	srcDir := t.TempDir()
	stagingDir := filepath.Join(t.TempDir(), "staging")

	var paths []string
	for i := 1; i <= 20; i++ {
		paths = append(paths, writeShard(t, srcDir, i, 1, 900))
	}

	im := NewImporter(svc, 6*1024)
	rep, err := im.ImportShards(paths, stagingDir, func() time.Time { return at(6) })
	if err != nil {
		t.Fatalf("导入 20 个小分片失败: %v（峰值占用 %d 字节，上限 %d 字节）",
			err, rep.PeakStagingBytes, im.StagingLimitBytes)
	}
	if rep.Shards != 20 {
		t.Fatalf("导入分片数 = %d, 期望 20", rep.Shards)
	}
}

func TestImportShardsSingleShard(t *testing.T) {
	svc := fixture(t)
	srcDir := t.TempDir()
	stagingDir := filepath.Join(t.TempDir(), "staging")
	path := writeShard(t, srcDir, 1, 3, 500)

	im := NewImporter(svc, 8*1024)
	rep, err := im.ImportShards([]string{path}, stagingDir, func() time.Time { return at(6) })
	if err != nil {
		t.Fatalf("导入单个分片失败: %v", err)
	}
	if rep.Shards != 1 || rep.Records != 3 {
		t.Fatalf("导入结果 = %+v", rep)
	}
	if len(rep.Breaches) != 3 {
		t.Fatalf("生成溃口数 = %d, 期望 3", len(rep.Breaches))
	}
}

func TestImportShardsRejectsEmptyList(t *testing.T) {
	svc := fixture(t)
	im := NewImporter(svc, 1024)
	if _, err := im.ImportShards(nil, t.TempDir(), func() time.Time { return at(6) }); err == nil {
		t.Fatalf("空列表应返回错误")
	}
}

func TestImportShardsRejectsBadShard(t *testing.T) {
	svc := fixture(t)
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	im := NewImporter(svc, 8*1024)
	if _, err := im.ImportShards([]string{bad}, filepath.Join(dir, "staging"), func() time.Time { return at(6) }); err == nil {
		t.Fatalf("非法分片应返回错误")
	}
}

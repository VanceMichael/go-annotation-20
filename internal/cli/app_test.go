package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"floodwatch/internal/seed"
)

func run(t *testing.T, args ...string) (int, map[string]any, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	var payload map[string]any
	if stdout.Len() > 0 {
		if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
			payload = nil
		}
	}
	return code, payload, stderr.String()
}

func TestHelpAndVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(nil, &stdout, &stderr); code != ExitOK {
		t.Fatalf("无参数退出码 = %d", code)
	}
	if stdout.Len() == 0 {
		t.Fatalf("应输出用法说明")
	}
	stdout.Reset()
	if code := Run([]string{"version"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("version 退出码 = %d", code)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(Version)) {
		t.Fatalf("version 输出 = %q", stdout.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	if code, _, _ := run(t, "teleport"); code != ExitUsage {
		t.Fatalf("退出码 = %d, 期望 %d", code, ExitUsage)
	}
}

// TestWarningAssessAtGuaranteeLevel 断言水位恰好达到保证水位时判定为红色预警。
func TestWarningAssessAtGuaranteeLevel(t *testing.T) {
	code, payload, stderr := run(t, "warning", "assess", "--code", "HN-JLH-01", "--level", "25.0")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if payload["level"] != "red" {
		t.Fatalf("水位 25.0m（保证水位）判定为 %v, 期望 red", payload["level"])
	}
	if payload["response"] != "level-1" {
		t.Fatalf("响应级别 = %v, 期望 level-1", payload["response"])
	}
}

// TestWarningAssessAtWarnLevel 断言水位恰好达到警戒水位时判定为蓝色预警。
func TestWarningAssessAtWarnLevel(t *testing.T) {
	code, payload, stderr := run(t, "warning", "assess", "--code", "HN-JLH-01", "--level", "22.0")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if payload["level"] != "blue" {
		t.Fatalf("水位 22.0m（警戒水位）判定为 %v, 期望 blue", payload["level"])
	}
}

// TestWarningAssessAtOrangeThreshold 断言恰好达到橙色阈值时判定为橙色预警。
func TestWarningAssessAtOrangeThreshold(t *testing.T) {
	code, payload, stderr := run(t, "warning", "assess", "--code", "HN-JLH-01", "--level", "24.0")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if payload["level"] != "orange" {
		t.Fatalf("水位 24.0m（橙色阈值）判定为 %v, 期望 orange", payload["level"])
	}
}

// TestStationDischargeOnUnratedStation 断言未建曲线的站点推算流量返回退出码 6 且不 panic。
func TestStationDischargeOnUnratedStation(t *testing.T) {
	code, payload, stderr := run(t, "station", "discharge", "--code", "HN-QYH-02")
	if code != ExitUnprocessable {
		t.Fatalf("退出码 = %d, 期望 %d; stdout=%+v stderr=%s", code, ExitUnprocessable, payload, stderr)
	}
	if payload == nil {
		t.Fatalf("应有 JSON 输出, stderr=%s", stderr)
	}
	if ok, _ := payload["ok"].(bool); ok {
		t.Fatalf("未建曲线站点不应返回成功: %+v", payload)
	}
	if hasRating, _ := payload["has_rating"].(bool); hasRating {
		t.Fatalf("HN-QYH-02 未布设测流断面, has_rating 应为 false")
	}
}

// TestStationDischargeOnRatedStation 断言建有曲线的站点可以正常推算流量。
func TestStationDischargeOnRatedStation(t *testing.T) {
	code, payload, stderr := run(t, "station", "discharge", "--code", "HN-JLH-01")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stdout=%+v stderr=%s", code, payload, stderr)
	}
	if q, _ := payload["discharge_m3s"].(float64); q <= 0 {
		t.Fatalf("流量 = %v, 应为正", payload["discharge_m3s"])
	}
}

// TestStationShowUnratedStation 断言查看未建曲线站点不 panic 且能给出预警判定。
func TestStationShowUnratedStation(t *testing.T) {
	code, payload, stderr := run(t, "station", "show", "--code", "ZJ-TXH-05")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stdout=%+v stderr=%s", code, payload, stderr)
	}
	if hasRating, _ := payload["has_rating"].(bool); hasRating {
		t.Fatalf("ZJ-TXH-05 应无曲线")
	}
	if payload["assessment"] == nil {
		t.Fatalf("应包含预警判定: %+v", payload)
	}
}

func TestStationNotFound(t *testing.T) {
	if code, _, _ := run(t, "station", "show", "--code", "NOPE"); code != ExitNotFound {
		t.Fatalf("退出码 = %d, 期望 %d", code, ExitNotFound)
	}
}

// TestSeriesAnalyseKeepsInputUnchanged 断言序列分析不改动原始观测数据。
func TestSeriesAnalyseKeepsInputUnchanged(t *testing.T) {
	code, payload, stderr := run(t, "series", "analyse", "--window", "3")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, 期望 0; stdout=%+v stderr=%s", code, payload, stderr)
	}
	if unchanged, _ := payload["input_unchanged"].(bool); !unchanged {
		t.Fatalf("input_unchanged = false, 分析改动了原始观测数据: %+v", payload)
	}
	if chrono, _ := payload["input_chronological"].(bool); !chrono {
		t.Fatalf("input_chronological = false, 分析打乱了序列时间顺序: %+v", payload)
	}
}

// TestSeriesAnalyseRateFromOriginalOrder 断言涨落率基于原始时间顺序首末两条计算。
func TestSeriesAnalyseRateFromOriginalOrder(t *testing.T) {
	code, payload, stderr := run(t, "series", "analyse", "--window", "3")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	from, _ := payload["rate_from_m"].(float64)
	to, _ := payload["rate_to_m"].(float64)
	if from != 21.10 {
		t.Fatalf("起始水位 = %v, 期望 21.10（序列首条）", payload["rate_from_m"])
	}
	if to != 25.00 {
		t.Fatalf("结束水位 = %v, 期望 25.00（序列末条）", payload["rate_to_m"])
	}
	if rising, _ := payload["rising"].(bool); !rising {
		t.Fatalf("序列整体上涨, rising 应为 true")
	}
	if peak, _ := payload["peak_m"].(float64); peak != 25.00 {
		t.Fatalf("峰值 = %v, 期望 25.00", payload["peak_m"])
	}
}

func TestSeriesAnalyseBadWindow(t *testing.T) {
	if code, _, _ := run(t, "series", "analyse", "--window", "0"); code != ExitBadRequest {
		t.Fatalf("退出码 = %d, 期望 %d", code, ExitBadRequest)
	}
}

// TestBreachComposeDeterministicErrorFailsFast 断言上游回复河段不存在时立刻失败、退出码 5。
func TestBreachComposeDeterministicErrorFailsFast(t *testing.T) {
	code, payload, stderr := run(t, "breach", "compose", "--upstream", "unknown-reach")
	if code != ExitNotFound {
		t.Fatalf("退出码 = %d, 期望 %d（河段不存在）; stdout=%+v stderr=%s", code, ExitNotFound, payload, stderr)
	}
	if ok, _ := payload["ok"].(bool); ok {
		t.Fatalf("上游回复河段不存在时不应成功: %+v", payload)
	}
	// 确定性错误不得进入退避重试，因此耗时应远小于一次退避等待的累计值。
	if elapsed, _ := payload["elapsed_ms"].(float64); elapsed > 15 {
		t.Fatalf("耗时 = %v ms, 期望接近 0（确定性错误不应重试）", elapsed)
	}
}

// TestBreachComposeUpstreamTimeoutRetries 断言上游临时故障走退避重试并最终报超时。
func TestBreachComposeUpstreamTimeoutRetries(t *testing.T) {
	code, payload, stderr := run(t, "breach", "compose", "--upstream", "timeout")
	if code != ExitUpstream {
		t.Fatalf("退出码 = %d, 期望 %d（上游超时）; stdout=%+v stderr=%s", code, ExitUpstream, payload, stderr)
	}
	if ok, _ := payload["ok"].(bool); ok {
		t.Fatalf("上游持续超时不应成功: %+v", payload)
	}
}

// TestBreachComposeFlakyUpstreamRecovers 断言上游前两次超时、第三次恢复时编排最终成功。
func TestBreachComposeFlakyUpstreamRecovers(t *testing.T) {
	code, payload, stderr := run(t, "breach", "compose", "--upstream", "flaky", "--width", "25")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, 期望 0（临时故障应退避重试后成功）; stdout=%+v stderr=%s",
			code, payload, stderr)
	}
	if ok, _ := payload["ok"].(bool); !ok {
		t.Fatalf("ok = false: %+v", payload)
	}
	if attempts, _ := payload["attempts"].(float64); attempts != 3 {
		t.Fatalf("attempts = %v, 期望 3（前两次超时需重试）", payload["attempts"])
	}
}

func TestBreachComposeOK(t *testing.T) {
	code, payload, stderr := run(t, "breach", "compose", "--upstream", "ok", "--width", "25")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stdout=%+v stderr=%s", code, payload, stderr)
	}
	if attempts, _ := payload["attempts"].(float64); attempts != 1 {
		t.Fatalf("attempts = %v, 期望 1", payload["attempts"])
	}
}

func TestBreachComposeBadUpstream(t *testing.T) {
	if code, _, _ := run(t, "breach", "compose", "--upstream", "nope"); code != ExitBadRequest {
		t.Fatalf("退出码 = %d, 期望 %d", code, ExitBadRequest)
	}
}

func TestBreachComposeCapacityShort(t *testing.T) {
	if code, _, _ := run(t, "breach", "compose", "--width", "500"); code != ExitConflict {
		t.Fatalf("退出码 = %d, 期望 %d", code, ExitConflict)
	}
}

// TestBreachImportManyShards 断言逐片导入 8 个分片时作业目录占用不累积。
func TestBreachImportManyShards(t *testing.T) {
	code, payload, stderr := run(t,
		"breach", "import",
		"--shards", "8", "--records-per-shard", "2",
		"--shard-padding", "3000", "--staging-limit", "8192",
	)
	if code != ExitOK {
		t.Fatalf("退出码 = %d, 期望 0; stdout=%+v stderr=%s", code, payload, stderr)
	}
	imported, _ := payload["shards_imported"].(float64)
	if imported != 8 {
		t.Fatalf("导入分片数 = %v, 期望 8", payload["shards_imported"])
	}
	peak, _ := payload["peak_staging_bytes"].(float64)
	if peak > 6144 {
		t.Fatalf("作业目录峰值占用 = %v 字节, 期望不超过单片规模（约 3KB）", peak)
	}
}

// TestBreachImportSingleShard 断言单分片导入正常。
func TestBreachImportSingleShard(t *testing.T) {
	code, payload, stderr := run(t,
		"breach", "import",
		"--shards", "1", "--records-per-shard", "3",
		"--shard-padding", "3000", "--staging-limit", "8192",
	)
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stdout=%+v stderr=%s", code, payload, stderr)
	}
	if records, _ := payload["records"].(float64); records != 3 {
		t.Fatalf("导入记录数 = %v, 期望 3", payload["records"])
	}
}

func TestBreachImportBadArgs(t *testing.T) {
	if code, _, _ := run(t, "breach", "import", "--shards", "0"); code != ExitBadRequest {
		t.Fatalf("退出码 = %d, 期望 %d", code, ExitBadRequest)
	}
}

// TestReportSituationEscalation 断言态势报表在有站点达到保证水位时启动一级响应。
func TestReportSituationEscalation(t *testing.T) {
	code, payload, stderr := run(t, "report", "situation")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if payload["highest_level"] != "red" {
		t.Fatalf("最高等级 = %v, 期望 red", payload["highest_level"])
	}
	if payload["response"] != "level-1" {
		t.Fatalf("响应级别 = %v, 期望 level-1", payload["response"])
	}
	if stations, _ := payload["stations"].([]any); len(stations) != len(seed.Stations()) {
		t.Fatalf("报表行数 = %d, 期望 %d", len(stations), len(seed.Stations()))
	}
}

func TestReportBasins(t *testing.T) {
	code, payload, stderr := run(t, "report", "basins")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if basins, _ := payload["basins"].([]any); len(basins) != 5 {
		t.Fatalf("流域行数 = %d, 期望 5", len(basins))
	}
}

// TestSelfcheckPasses 断言内置自检全部通过。
func TestSelfcheckPasses(t *testing.T) {
	code, payload, stderr := run(t, "selfcheck")
	if code != ExitOK {
		t.Fatalf("自检退出码 = %d, 期望 0; stdout=%+v stderr=%s", code, payload, stderr)
	}
	if failed, _ := payload["failed"].(float64); failed != 0 {
		t.Fatalf("自检失败项 = %v, 期望 0; 输出 %+v", payload["failed"], payload)
	}
}

func TestListCommands(t *testing.T) {
	code, payload, stderr := run(t, "station", "list")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if stations, _ := payload["stations"].([]any); len(stations) != len(seed.Stations()) {
		t.Fatalf("站点数 = %d", len(stations))
	}
	if rated, _ := payload["with_rating"].([]any); len(rated) != 4 {
		t.Fatalf("建有曲线站点数 = %d, 期望 4", len(rated))
	}

	code, payload, stderr = run(t, "reach", "list")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if reaches, _ := payload["reaches"].([]any); len(reaches) != len(seed.Reaches()) {
		t.Fatalf("河段数 = %d", len(reaches))
	}

	code, payload, stderr = run(t, "crew", "list")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if crews, _ := payload["crews"].([]any); len(crews) != len(seed.Crews()) {
		t.Fatalf("队伍数 = %d", len(crews))
	}
}

func TestStationThresholds(t *testing.T) {
	code, payload, stderr := run(t, "station", "thresholds", "--code", "HN-JLH-01")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if items, _ := payload["thresholds"].([]any); len(items) != 4 {
		t.Fatalf("阈值档数 = %d, 期望 4", len(items))
	}
}

func TestStationListByBasin(t *testing.T) {
	code, payload, stderr := run(t, "station", "list", "--basin", "huaihe")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if stations, _ := payload["stations"].([]any); len(stations) != 2 {
		t.Fatalf("淮河流域站点数 = %d, 期望 2", len(stations))
	}
	if code, _, _ := run(t, "station", "list", "--basin", "nope"); code != ExitBadRequest {
		t.Fatalf("非法流域退出码 = %d, 期望 %d", code, ExitBadRequest)
	}
}

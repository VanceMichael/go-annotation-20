// Package cli 实现 floodctl 命令行界面。
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"floodwatch/internal/breach"
	"floodwatch/internal/gauge"
	"floodwatch/internal/httpapi"
	"floodwatch/internal/model"
	"floodwatch/internal/reach"
	"floodwatch/internal/report"
	"floodwatch/internal/seed"
	"floodwatch/internal/warning"
)

// 退出码约定。上层脚本依赖这些取值区分失败类别。
const (
	// ExitOK 正常结束。
	ExitOK = 0
	// ExitUsage 命令行用法错误或未归类的内部错误。
	ExitUsage = 1
	// ExitBadRequest 参数非法。
	ExitBadRequest = 2
	// ExitConflict 业务冲突：状态流转冲突、抢险力量不足、站点离线等。
	ExitConflict = 3
	// ExitUpstream 上游数据服务超时或作业空间超限。
	ExitUpstream = 4
	// ExitNotFound 资源不存在。
	ExitNotFound = 5
	// ExitUnprocessable 数据不足以处理：缺少曲线、缺少观测。
	ExitUnprocessable = 6
)

// Version 是当前构建版本。
const Version = "0.5.0"

const usage = `floodctl —— 防汛水情监测与溃口抢险调度平台命令行

用法:
  floodctl <命令> [子命令] [参数]

命令:
  station list        列出水位站
  station show        查看单站水情与预警判定
  station discharge   推算单站流量
  station thresholds  输出单站分档预警阈值
  reach list          列出河段
  series analyse      分析内置观测序列（只读）
  warning assess      按给定水位判定预警等级
  report situation    生成流域水情态势报表
  report basins       生成流域维度报表
  breach report       登记溃口
  breach compose      编排抢险力量
  breach import       导入溃口上报分片
  crew list           列出抢险队伍
  serve               启动 HTTP 服务
  selfcheck           运行内置自检
  version             输出版本信息

退出码:
  0 成功  1 用法或内部错误  2 参数非法  3 业务冲突
  4 上游超时或作业空间超限  5 资源不存在  6 数据不足
`

type app struct {
	registry *gauge.Registry
	breaches *breach.Service
	reports  *report.Builder
	stdout   io.Writer
	stderr   io.Writer
}

func newApp(stdout, stderr io.Writer) (*app, error) {
	reg, svc, err := seed.Load()
	if err != nil {
		return nil, err
	}
	return &app{
		registry: reg,
		breaches: svc,
		reports:  report.NewBuilder(reg),
		stdout:   stdout,
		stderr:   stderr,
	}, nil
}

// Run 执行一次命令行调用并返回退出码。
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, usage)
		return ExitOK
	}
	a, err := newApp(stdout, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "初始化失败: %v\n", err)
		return ExitUsage
	}
	code, err := a.route(args)
	if err != nil {
		fmt.Fprintf(stderr, "错误: %v\n", err)
	}
	return code
}

func (a *app) route(args []string) (int, error) {
	switch args[0] {
	case "version":
		fmt.Fprintf(a.stdout, "floodctl %s\n", Version)
		return ExitOK, nil
	case "station":
		return a.runStation(args[1:])
	case "reach":
		return a.runReach(args[1:])
	case "series":
		return a.runSeries(args[1:])
	case "warning":
		return a.runWarning(args[1:])
	case "report":
		return a.runReport(args[1:])
	case "breach":
		return a.runBreach(args[1:])
	case "crew":
		return a.runCrew(args[1:])
	case "serve":
		return a.runServe(args[1:])
	case "selfcheck":
		return a.runSelfcheck(args[1:])
	default:
		fmt.Fprint(a.stderr, usage)
		return ExitUsage, fmt.Errorf("未知命令 %q", args[0])
	}
}

func (a *app) emit(payload any) error {
	enc := json.NewEncoder(a.stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// classify 把领域错误映射为退出码，映射依据是错误链中的哨兵错误。
func classify(err error) int {
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, model.ErrUpstreamTimeout), errors.Is(err, model.ErrStagingFull):
		return ExitUpstream
	case errors.Is(err, model.ErrNoRatingCurve), errors.Is(err, model.ErrNoReadings):
		return ExitUnprocessable
	case errors.Is(err, model.ErrStationOffline),
		errors.Is(err, model.ErrStateConflict),
		errors.Is(err, model.ErrCapacityShort),
		errors.Is(err, model.ErrCrewUnavailable):
		return ExitConflict
	case errors.Is(err, model.ErrStationUnknown),
		errors.Is(err, model.ErrReachUnknown),
		errors.Is(err, model.ErrBreachUnknown),
		errors.Is(err, model.ErrCrewUnknown):
		return ExitNotFound
	case errors.Is(err, model.ErrInvalidStation),
		errors.Is(err, model.ErrInvalidReading),
		errors.Is(err, model.ErrUnknownBasin),
		errors.Is(err, model.ErrUnknownLevel):
		return ExitBadRequest
	default:
		return ExitUsage
	}
}

func (a *app) runStation(args []string) (int, error) {
	if len(args) == 0 {
		return ExitUsage, errors.New("station 需要子命令: list / show / discharge / thresholds")
	}
	fs := flag.NewFlagSet("station "+args[0], flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	codeFlag := fs.String("code", "", "站码")
	basinFlag := fs.String("basin", "", "按流域筛选")
	levelFlag := fs.Float64("level", -1, "指定水位，默认取内置当前水位")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}

	resolveLevel := func(code string) (float64, bool) {
		if *levelFlag >= 0 {
			return *levelFlag, true
		}
		v, ok := seed.Levels()[code]
		return v, ok
	}

	switch args[0] {
	case "list":
		if *basinFlag != "" {
			b, err := model.ParseBasin(*basinFlag)
			if err != nil {
				return classify(err), err
			}
			return ExitOK, a.emit(map[string]any{"stations": a.registry.StationsByBasin(b)})
		}
		return ExitOK, a.emit(map[string]any{
			"stations":    a.registry.Stations(),
			"with_rating": a.registry.RatingStations(),
		})

	case "show":
		if *codeFlag == "" {
			return ExitBadRequest, errors.New("station show 需要 --code")
		}
		st, err := a.registry.Station(*codeFlag)
		if err != nil {
			return classify(err), err
		}
		payload := map[string]any{
			"station":    st,
			"has_rating": st.HasRating(),
		}
		if levelM, ok := resolveLevel(st.Code); ok {
			as, aerr := warning.Assess(levelM, st)
			if aerr != nil {
				return classify(aerr), aerr
			}
			payload["assessment"] = as
		}
		return ExitOK, a.emit(payload)

	case "discharge":
		if *codeFlag == "" {
			return ExitBadRequest, errors.New("station discharge 需要 --code")
		}
		st, err := a.registry.Station(*codeFlag)
		if err != nil {
			return classify(err), err
		}
		levelM, ok := resolveLevel(st.Code)
		if !ok {
			return ExitBadRequest, fmt.Errorf("站点 %s 无内置水位, 请用 --level 指定", st.Code)
		}
		q, derr := gauge.Discharge(st, levelM)
		payload := map[string]any{
			"station_code": st.Code,
			"level_m":      levelM,
			"has_rating":   st.HasRating(),
			"ok":           derr == nil,
		}
		if derr != nil {
			payload["message"] = derr.Error()
		} else {
			payload["discharge_m3s"] = q
		}
		if err := a.emit(payload); err != nil {
			return ExitUsage, err
		}
		if derr != nil {
			return classify(derr), derr
		}
		return ExitOK, nil

	case "thresholds":
		if *codeFlag == "" {
			return ExitBadRequest, errors.New("station thresholds 需要 --code")
		}
		st, err := a.registry.Station(*codeFlag)
		if err != nil {
			return classify(err), err
		}
		items, terr := warning.Thresholds(st)
		if terr != nil {
			return classify(terr), terr
		}
		return ExitOK, a.emit(map[string]any{"station_code": st.Code, "thresholds": items})

	default:
		return ExitUsage, fmt.Errorf("未知子命令 station %q", args[0])
	}
}

func (a *app) runReach(args []string) (int, error) {
	if len(args) == 0 || args[0] != "list" {
		return ExitUsage, errors.New("reach 需要子命令: list")
	}
	return ExitOK, a.emit(map[string]any{"reaches": a.registry.Reaches()})
}

// runSeries 分析内置观测序列。分析是只读操作，命令会同时输出
// 分析前后序列是否保持一致，用于核对处理过程没有改动原始数据。
func (a *app) runSeries(args []string) (int, error) {
	if len(args) == 0 || args[0] != "analyse" {
		return ExitUsage, errors.New("series 需要子命令: analyse")
	}
	fs := flag.NewFlagSet("series analyse", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	windowFlag := fs.Int("window", 3, "平滑窗口长度")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}
	if *windowFlag <= 0 {
		return ExitBadRequest, errors.New("--window 必须为正")
	}

	readings := seed.Readings()
	before := make([]model.Reading, len(readings))
	copy(before, readings)

	sum, err := reach.Analyse(readings, *windowFlag)
	if err != nil {
		return classify(err), err
	}

	unchanged := true
	for i := range before {
		if readings[i] != before[i] {
			unchanged = false
			break
		}
	}

	payload := map[string]any{
		"station_code":        sum.StationCode,
		"count":               sum.Count,
		"peak_m":              sum.PeakM,
		"peak_at":             sum.PeakAt,
		"rainfall_mm":         sum.RainfallMM,
		"rate_from_m":         sum.Rate.FromM,
		"rate_to_m":           sum.Rate.ToM,
		"rate_hours":          sum.Rate.Hours,
		"rate_m_per_hour":     sum.Rate.RateMPerHour,
		"rising":              sum.Rate.Rising,
		"input_chronological": reach.Chronological(readings),
		"input_unchanged":     unchanged,
	}
	if err := a.emit(payload); err != nil {
		return ExitUsage, err
	}
	if !unchanged || !reach.Chronological(readings) {
		return ExitUsage, errors.New("序列分析改动了原始观测数据")
	}
	return ExitOK, nil
}

func (a *app) runWarning(args []string) (int, error) {
	if len(args) == 0 || args[0] != "assess" {
		return ExitUsage, errors.New("warning 需要子命令: assess")
	}
	fs := flag.NewFlagSet("warning assess", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	codeFlag := fs.String("code", "", "站码")
	levelFlag := fs.Float64("level", -1, "水位，单位米")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}
	if *codeFlag == "" || *levelFlag < 0 {
		return ExitBadRequest, errors.New("warning assess 需要 --code 和 --level")
	}
	st, err := a.registry.Station(*codeFlag)
	if err != nil {
		return classify(err), err
	}
	as, aerr := warning.Assess(*levelFlag, st)
	if aerr != nil {
		return classify(aerr), aerr
	}
	return ExitOK, a.emit(as)
}

func (a *app) runReport(args []string) (int, error) {
	if len(args) == 0 {
		return ExitUsage, errors.New("report 需要子命令: situation 或 basins")
	}
	switch args[0] {
	case "situation":
		sit, err := a.reports.Situation(seed.Levels())
		if err != nil {
			return classify(err), err
		}
		return ExitOK, a.emit(sit)
	case "basins":
		lines, err := a.reports.Basins(seed.Levels())
		if err != nil {
			return classify(err), err
		}
		return ExitOK, a.emit(map[string]any{"basins": lines})
	default:
		return ExitUsage, fmt.Errorf("未知子命令 report %q", args[0])
	}
}

func (a *app) runBreach(args []string) (int, error) {
	if len(args) == 0 {
		return ExitUsage, errors.New("breach 需要子命令: report / compose / import")
	}
	fs := flag.NewFlagSet("breach "+args[0], flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	reachFlag := fs.String("reach", "R-JLH-01", "河段代码")
	locationFlag := fs.String("location", "左岸 K12+300", "溃口位置")
	widthFlag := fs.Float64("width", 25, "溃口宽度，单位米")
	upstreamFlag := fs.String("upstream", "ok", "上游演练模式: ok / unknown-reach / timeout")
	shardsFlag := fs.Int("shards", 8, "导入分片数量")
	recordsFlag := fs.Int("records-per-shard", 2, "每个分片的上报条数")
	padFlag := fs.Int("shard-padding", 3000, "单个分片附带原始报文的字节数")
	limitFlag := fs.Int64("staging-limit", 8*1024, "临时作业目录容量上限，单位字节")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}

	switch args[0] {
	case "report":
		b, err := a.breaches.Report(*reachFlag, *locationFlag, *widthFlag, time.Date(2026, time.August, 16, 6, 0, 0, 0, time.UTC))
		if err != nil {
			return classify(err), err
		}
		return ExitOK, a.emit(b)

	case "compose":
		b, err := a.breaches.Report(*reachFlag, *locationFlag, *widthFlag, time.Date(2026, time.August, 16, 6, 0, 0, 0, time.UTC))
		if err != nil {
			return classify(err), err
		}
		probe, perr := probeFor(*upstreamFlag)
		if perr != nil {
			return ExitBadRequest, perr
		}

		begin := time.Now()
		plan, cerr := a.breaches.Compose(b.ID, probe)
		elapsed := time.Since(begin)

		payload := map[string]any{
			"breach_id":  b.ID,
			"upstream":   *upstreamFlag,
			"elapsed_ms": elapsed.Milliseconds(),
			"ok":         cerr == nil,
		}
		if cerr != nil {
			payload["message"] = cerr.Error()
			payload["exit_code"] = classify(cerr)
		} else {
			payload["plan"] = plan
			payload["attempts"] = plan.Attempts
		}
		if err := a.emit(payload); err != nil {
			return ExitUsage, err
		}
		if cerr != nil {
			return classify(cerr), cerr
		}
		return ExitOK, nil

	case "import":
		if *shardsFlag <= 0 || *recordsFlag <= 0 {
			return ExitBadRequest, errors.New("--shards 与 --records-per-shard 必须为正")
		}
		workDir, terr := os.MkdirTemp("", "floodctl-import-")
		if terr != nil {
			return ExitUsage, terr
		}
		defer os.RemoveAll(workDir)

		srcDir := filepath.Join(workDir, "src")
		stagingDir := filepath.Join(workDir, "staging")
		if err := os.MkdirAll(srcDir, 0o755); err != nil {
			return ExitUsage, err
		}

		paths, gerr := generateShards(srcDir, *reachFlag, *shardsFlag, *recordsFlag, *padFlag)
		if gerr != nil {
			return ExitUsage, gerr
		}

		im := breach.NewImporter(a.breaches, *limitFlag)
		rep, ierr := im.ImportShards(paths, stagingDir, func() time.Time {
			return time.Date(2026, time.August, 16, 6, 0, 0, 0, time.UTC)
		})

		payload := map[string]any{
			"shards_requested":   *shardsFlag,
			"shards_imported":    rep.Shards,
			"records":            rep.Records,
			"peak_staging_bytes": rep.PeakStagingBytes,
			"staging_limit":      *limitFlag,
			"ok":                 ierr == nil,
		}
		if ierr != nil {
			payload["message"] = ierr.Error()
			payload["exit_code"] = classify(ierr)
		}
		if err := a.emit(payload); err != nil {
			return ExitUsage, err
		}
		if ierr != nil {
			return classify(ierr), ierr
		}
		return ExitOK, nil

	default:
		return ExitUsage, fmt.Errorf("未知子命令 breach %q", args[0])
	}
}

func (a *app) runCrew(args []string) (int, error) {
	if len(args) == 0 || args[0] != "list" {
		return ExitUsage, errors.New("crew 需要子命令: list")
	}
	return ExitOK, a.emit(map[string]any{"crews": a.breaches.Crews()})
}

func (a *app) runServe(args []string) (int, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	addrFlag := fs.String("addr", "127.0.0.1:8080", "监听地址")
	if err := fs.Parse(args); err != nil {
		return ExitUsage, err
	}
	srv := httpapi.New(httpapi.Options{
		Registry: a.registry,
		Breaches: a.breaches,
		Levels:   seed.Levels(),
		Readings: seed.Readings(),
	})
	fmt.Fprintf(a.stdout, "floodctl serve 监听 %s\n", *addrFlag)
	server := &http.Server{
		Addr:              *addrFlag,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return ExitUsage, err
	}
	return ExitOK, nil
}

func (a *app) runSelfcheck(args []string) (int, error) {
	fs := flag.NewFlagSet("selfcheck", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	if err := fs.Parse(args); err != nil {
		return ExitUsage, err
	}

	checks := make([]map[string]any, 0, 8)
	add := func(name string, ok bool, detail string) {
		checks = append(checks, map[string]any{"check": name, "ok": ok, "detail": detail})
	}

	c := a.registry.Counts()
	add("registry", c.Stations == len(seed.Stations()) && c.Reaches == len(seed.Reaches()),
		fmt.Sprintf("水位站 %d 个, 河段 %d 个", c.Stations, c.Reaches))

	// 恰好达到保证水位应判定为红色预警。
	jlh, err := a.registry.Station("HN-JLH-01")
	if err != nil {
		return classify(err), err
	}
	atGuarantee := warning.LevelFor(jlh.GuaranteeLevel, jlh)
	add("threshold-at-guarantee", atGuarantee == model.LevelRed,
		fmt.Sprintf("水位 %.2fm（保证水位）判定为 %s, 期望 red", jlh.GuaranteeLevel, atGuarantee))

	// 恰好达到警戒水位应判定为蓝色预警。
	atWarn := warning.LevelFor(jlh.WarnLevel, jlh)
	add("threshold-at-warn", atWarn == model.LevelBlue,
		fmt.Sprintf("水位 %.2fm（警戒水位）判定为 %s, 期望 blue", jlh.WarnLevel, atWarn))

	// 未建曲线的站点推算流量必须返回哨兵错误而非 panic。
	qyh, err := a.registry.Station("HN-QYH-02")
	if err != nil {
		return classify(err), err
	}
	_, derr := gauge.Discharge(qyh, 20.5)
	add("unrated-station-discharge", errors.Is(derr, model.ErrNoRatingCurve),
		fmt.Sprintf("未建曲线站点推算流量返回 %v", derr))

	// 序列分析必须是只读的。
	readings := seed.Readings()
	before := make([]model.Reading, len(readings))
	copy(before, readings)
	if _, aerr := reach.Analyse(readings, 3); aerr != nil {
		return classify(aerr), aerr
	}
	unchanged := true
	for i := range before {
		if readings[i] != before[i] {
			unchanged = false
			break
		}
	}
	add("series-analysis-readonly", unchanged && reach.Chronological(readings),
		fmt.Sprintf("分析后序列未改动 = %v, 仍为时序 = %v", unchanged, reach.Chronological(readings)))

	// 上游确定性错误不得重试。
	probeCalls := 0
	detProbe := func(code string) error {
		probeCalls++
		return fmt.Errorf("upstream: 核对河段 %s 失败: %w", code, model.ErrReachUnknown)
	}
	probeBreach, berr := a.breaches.Report("R-JLH-01", "自检位置", 20, time.Date(2026, time.August, 16, 6, 0, 0, 0, time.UTC))
	if berr != nil {
		return classify(berr), berr
	}
	_, cerr := a.breaches.Compose(probeBreach.ID, detProbe)
	add("deterministic-error-no-retry", probeCalls == 1 && errors.Is(cerr, model.ErrReachUnknown),
		fmt.Sprintf("确定性错误下上游被调用 %d 次（期望 1）, 错误可识别 = %v",
			probeCalls, errors.Is(cerr, model.ErrReachUnknown)))

	// 上游临时故障必须退避重试，恢复后编排成功。
	flakyCalls := 0
	flakyProbe := func(string) error {
		flakyCalls++
		if flakyCalls < 3 {
			return fmt.Errorf("upstream: 网关第 %d 次无响应: %w", flakyCalls, model.ErrUpstreamTimeout)
		}
		return nil
	}
	flakyBreach, ferr := a.breaches.Report("R-JLH-01", "自检位置", 20, time.Date(2026, time.August, 16, 6, 0, 0, 0, time.UTC))
	if ferr != nil {
		return classify(ferr), ferr
	}
	_, flakyErr := a.breaches.Compose(flakyBreach.ID, flakyProbe)
	add("transient-error-retries", flakyErr == nil && flakyCalls == 3,
		fmt.Sprintf("临时故障下上游被调用 %d 次（期望 3）, 最终结果 = %v", flakyCalls, flakyErr))

	// 分片导入过程中作业目录占用不得超过上限。
	workDir, terr := os.MkdirTemp("", "floodctl-selfcheck-")
	if terr != nil {
		return ExitUsage, terr
	}
	defer os.RemoveAll(workDir)
	srcDir := filepath.Join(workDir, "src")
	if mkErr := os.MkdirAll(srcDir, 0o755); mkErr != nil {
		return ExitUsage, mkErr
	}
	paths, gerr := generateShards(srcDir, "R-JLH-01", 8, 1, 3000)
	if gerr != nil {
		return ExitUsage, gerr
	}
	im := breach.NewImporter(a.breaches, 8*1024)
	rep, ierr := im.ImportShards(paths, filepath.Join(workDir, "staging"), func() time.Time {
		return time.Date(2026, time.August, 16, 6, 0, 0, 0, time.UTC)
	})
	add("shard-import-staging", ierr == nil && rep.Shards == 8,
		fmt.Sprintf("导入 8 个分片结果 = %v, 峰值占用 %d 字节（上限 %d）",
			ierr, rep.PeakStagingBytes, im.StagingLimitBytes))

	sort.Slice(checks, func(i, j int) bool {
		return checks[i]["check"].(string) < checks[j]["check"].(string)
	})
	failed := 0
	for _, ck := range checks {
		if !ck["ok"].(bool) {
			failed++
		}
	}
	if err := a.emit(map[string]any{"checks": checks, "failed": failed}); err != nil {
		return ExitUsage, err
	}
	if failed > 0 {
		return ExitUsage, fmt.Errorf("自检失败 %d 项", failed)
	}
	return ExitOK, nil
}

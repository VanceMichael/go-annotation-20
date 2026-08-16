package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"floodwatch/internal/breach"
	"floodwatch/internal/model"
)

// probeFor 依据演练模式构造上游数据服务探测函数。
//
//	ok             上游正常
//	unknown-reach  上游明确回复河段不存在（确定性错误，不应重试）
//	timeout        上游持续无响应（临时故障，应退避重试至上限）
//	flaky          上游前两次无响应、第三次恢复（临时故障，重试后应成功）
func probeFor(mode string) (func(string) error, error) {
	switch mode {
	case "ok":
		return func(string) error { return nil }, nil
	case "unknown-reach":
		return func(code string) error {
			return fmt.Errorf("upstream: 核对河段 %s 失败: %w", code, model.ErrReachUnknown)
		}, nil
	case "timeout":
		return func(string) error {
			return fmt.Errorf("upstream: 网关无响应: %w", model.ErrUpstreamTimeout)
		}, nil
	case "flaky":
		calls := 0
		return func(string) error {
			calls++
			if calls < 3 {
				return fmt.Errorf("upstream: 网关第 %d 次无响应: %w", calls, model.ErrUpstreamTimeout)
			}
			return nil
		}, nil
	default:
		return nil, fmt.Errorf("--upstream 需为 ok / unknown-reach / timeout / flaky, 收到 %q", mode)
	}
}

// generateShards 在 dir 下生成用于演练的溃口上报分片文件。
// padding 用于模拟分片附带的原始报文，撑大单个分片的体积。
func generateShards(dir, reachCode string, shards, recordsPerShard, padding int) ([]string, error) {
	var paths []string
	for i := 1; i <= shards; i++ {
		sh := breach.Shard{ReachCode: reachCode}
		for j := 0; j < recordsPerShard; j++ {
			sh.Records = append(sh.Records, breach.ShardRecord{
				Location: fmt.Sprintf("左岸 K%02d+%03d", i, j*100),
				WidthM:   float64(8 + j),
				Source:   "巡堤查险",
			})
		}
		data, err := json.Marshal(sh)
		if err != nil {
			return nil, fmt.Errorf("cli: 序列化分片 %d 失败: %w", i, err)
		}
		if padding > 0 {
			data = append(data, make([]byte, padding)...)
		}
		path := filepath.Join(dir, fmt.Sprintf("shard-%02d.json", i))
		if werr := os.WriteFile(path, data, 0o644); werr != nil {
			return nil, fmt.Errorf("cli: 写分片 %d 失败: %w", i, werr)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

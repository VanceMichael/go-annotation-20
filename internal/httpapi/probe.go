package httpapi

import (
	"fmt"

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
		return nil, fmt.Errorf("httpapi: upstream 需为 ok / unknown-reach / timeout / flaky, 收到 %q", mode)
	}
}

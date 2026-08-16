package model

import "errors"

// 领域哨兵错误。上层通过 errors.Is 判定错误类别，禁止依赖错误文本，
// 也禁止用 == 直接比较（错误会被下层包装）。
var (
	// ErrInvalidStation 水位站登记信息非法。
	ErrInvalidStation = errors.New("model: 水位站登记信息非法")
	// ErrInvalidReading 观测记录非法。
	ErrInvalidReading = errors.New("model: 观测记录非法")
	// ErrUnknownBasin 流域代码不存在。
	ErrUnknownBasin = errors.New("model: 未知流域")
	// ErrUnknownLevel 预警等级代码不存在。
	ErrUnknownLevel = errors.New("model: 未知预警等级")
	// ErrStationUnknown 水位站不存在。
	ErrStationUnknown = errors.New("model: 水位站不存在")
	// ErrStationOffline 水位站离线。
	ErrStationOffline = errors.New("model: 水位站离线")
	// ErrReachUnknown 河段不存在。
	ErrReachUnknown = errors.New("model: 河段不存在")
	// ErrBreachUnknown 溃口不存在。
	ErrBreachUnknown = errors.New("model: 溃口不存在")
	// ErrCrewUnknown 抢险队伍不存在。
	ErrCrewUnknown = errors.New("model: 抢险队伍不存在")
	// ErrCrewUnavailable 抢险队伍当前不可用。
	ErrCrewUnavailable = errors.New("model: 抢险队伍当前不可用")
	// ErrNoRatingCurve 该站未建水位流量关系曲线。
	ErrNoRatingCurve = errors.New("model: 该站未建水位流量关系曲线")
	// ErrNoReadings 缺少观测数据。
	ErrNoReadings = errors.New("model: 缺少观测数据")
	// ErrStateConflict 状态流转非法。
	ErrStateConflict = errors.New("model: 溃口状态流转非法")
	// ErrCapacityShort 抢险力量不足。
	ErrCapacityShort = errors.New("model: 抢险力量不足")
	// ErrStagingFull 临时作业空间超限。
	ErrStagingFull = errors.New("model: 临时作业空间超限")
	// ErrUpstreamTimeout 上游数据服务超时。
	ErrUpstreamTimeout = errors.New("model: 上游数据服务超时")
)

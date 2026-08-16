# floodwatch —— 防汛水情监测与溃口抢险调度平台

`floodwatch` 是一个纯 Go 实现的后端与命令行工具，面向流域防汛，覆盖水位站观测、
水情要素推算、预警等级判定、河段序列分析、溃口抢险力量编排与上报分片导入。

项目不依赖任何第三方模块，只使用 Go 标准库。

## 业务背景

### 预警等级判定

按水位相对特征水位的位置划分，各档均为**达到即进入**，即水位恰好等于某档阈值时
就属于该档：

| 条件 | 等级 | 应急响应 |
| --- | --- | --- |
| 水位 ≥ 历史最高水位 | 红色预警 | 一级响应 |
| 水位 ≥ 保证水位 | 红色预警 | 一级响应 |
| 水位 ≥ 警戒水位 + 2/3 超警幅度 | 橙色预警 | 二级响应 |
| 水位 ≥ 警戒水位 + 1/3 超警幅度 | 黄色预警 | 三级响应 |
| 水位 ≥ 警戒水位 | 蓝色预警 | 四级响应 |
| 其余 | 无预警 | 未启动响应 |

超警幅度指保证水位与警戒水位之差。

### 水位流量关系曲线

只有布设了测流断面的水位站才建有水位流量关系曲线：

```
Q = C * (H - H0) ^ n
```

`C` 为流量系数，`n` 为水位差指数，`H0` 为断流水位。**未建曲线的站点没有曲线
参数**，对这类站点的流量查询必须返回 `model.ErrNoRatingCurve`。

### 溃口抢险编排的重试策略

编排前需向上游数据服务核对河段信息。只有上游临时故障（`model.ErrUpstreamTimeout`）
才允许退避重试，最多 3 次；河段不存在、溃口不存在这类确定性错误必须立刻返回。

### 上报分片导入

导入过程会为每个分片在临时作业目录中落一份解包副本。作业目录容量有限
（`--staging-limit`）：**导入过程中任意时刻的作业目录占用都不得超过该上限**，
超限时导入中止并返回退出码 4。导入正常结束后作业目录中不应留下残余副本。

### 序列分析的只读约定

中位数平滑、涨落率、峰值检测都是只读分析：传入的观测序列必须保持原有的
时间顺序与取值，不得被原地改动。

## 目录结构

```
cmd/floodctl          命令行入口
internal/model        领域模型与哨兵错误
internal/gauge        水位站台账与流量推算
internal/warning      预警等级判定
internal/reach        河段水位序列分析
internal/breach       溃口抢险登记、状态流转与力量编排
internal/report       水情态势与流域报表
internal/httpapi      HTTP 接口
internal/seed         内置样例数据
internal/cli          floodctl 命令实现
```

## 构建与测试

```bash
export GOTOOLCHAIN=local

go build ./...
go test ./...
go test -race ./...

make build           # 产出 bin/floodctl
make selfcheck       # 构建并运行内置自检
```

## 命令行用法

```bash
floodctl station list
floodctl station list --basin huaihe
floodctl station show --code HN-JLH-01
floodctl station discharge --code HN-JLH-01
floodctl station discharge --code HN-QYH-02      # 未建曲线，返回退出码 6
floodctl station thresholds --code HN-JLH-01

floodctl reach list
floodctl series analyse --window 3
floodctl warning assess --code HN-JLH-01 --level 25.0

floodctl report situation
floodctl report basins

floodctl breach report --reach R-JLH-01 --width 25
floodctl breach compose --upstream ok
floodctl breach compose --upstream unknown-reach   # 确定性错误，应立刻失败
floodctl breach compose --upstream timeout         # 临时故障，退避重试后报超时
floodctl breach import --shards 8 --records-per-shard 2 --staging-limit 8192

floodctl crew list
floodctl serve --addr 127.0.0.1:8080
floodctl selfcheck
```

### 退出码

| 退出码 | 含义 |
| --- | --- |
| 0 | 成功 |
| 1 | 用法错误或未归类的内部错误 |
| 2 | 参数非法 |
| 3 | 业务冲突（状态流转冲突、抢险力量不足、站点离线） |
| 4 | 上游数据服务超时或临时作业空间超限 |
| 5 | 资源不存在 |
| 6 | 数据不足以处理（缺少曲线、缺少观测） |

## HTTP 接口

```
GET  /healthz
GET  /api/stations[?basin=huaihe]
GET  /api/stations/{code}
GET  /api/stations/{code}/discharge[?level_m=25.0]
GET  /api/stations/{code}/thresholds
GET  /api/reaches
GET  /api/series/analyse[?window=3]
GET  /api/report/situation
GET  /api/report/basins
GET  /api/breaches
GET  /api/breaches/{id}
POST /api/breaches
POST /api/breaches/{id}/advance
POST /api/breaches/{id}/compose[?upstream=ok|unknown-reach|timeout]
GET  /api/crews
```

错误响应统一为：

```json
{ "error": { "code": "no_rating_curve", "message": "..." } }
```

状态码约定：`404` 资源不存在，`409` 业务冲突，`422` 数据不足，`400` 参数非法，
`502` 上游数据服务超时，`507` 作业空间超限，`503` 调用被取消或超时，
`500` 未归类的内部错误。

> 说明：HTTP 接口默认不带鉴权，仅面向内网或本地演练环境。若需暴露到公网，
> 必须在前置网关补充身份认证与访问控制。

## 容器运行

```bash
docker build -t floodwatch:local .
docker run --rm floodwatch:local selfcheck
docker run --rm -p 8080:8080 floodwatch:local serve --addr 0.0.0.0:8080
```

镜像基于 `golang:1.22` 构建、`distroless/static` 运行，同时支持
`linux/amd64` 与 `linux/arm64`：

```bash
docker build --platform linux/amd64 -t floodwatch:amd64 .
docker build --platform linux/arm64 -t floodwatch:arm64 .
```

## 数据来源

内置样例数据（`internal/seed`）包含 6 个水位站、3 个河段、5 支抢险队伍与
10 条观测记录，覆盖淮河、长江、珠江与东南诸河四个流域，
其中 2 个站点未布设测流断面。仅用于本地演练，不代表真实水情数据。

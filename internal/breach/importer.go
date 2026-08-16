package breach

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"floodwatch/internal/model"
)

// Shard 是一个溃口上报分片文件的内容。
type Shard struct {
	ReachCode string        `json:"reach_code"`
	Records   []ShardRecord `json:"records"`
}

// ShardRecord 是分片中的一条溃口上报。
type ShardRecord struct {
	Location string  `json:"location"`
	WidthM   float64 `json:"width_m"`
	Source   string  `json:"source"`
}

// ImportReport 是分片导入结果。
type ImportReport struct {
	Shards   int      `json:"shards"`
	Records  int      `json:"records"`
	Breaches []string `json:"breaches"`
	// PeakStagingBytes 是导入过程中临时作业目录占用的峰值字节数。
	PeakStagingBytes int64 `json:"peak_staging_bytes"`
}

// Importer 负责把分片文件导入为溃口台账记录。
//
// 导入过程会为每个分片在临时作业目录中落一份解包副本。作业目录容量有限：
// 导入过程中任意时刻的目录占用都不得超过 StagingLimitBytes，
// 导入正常结束后作业目录中不应留下残余副本。
type Importer struct {
	svc *Service
	// StagingLimitBytes 是临时作业目录的容量上限。
	StagingLimitBytes int64
}

// NewImporter 构造分片导入器。
func NewImporter(svc *Service, stagingLimitBytes int64) *Importer {
	if stagingLimitBytes <= 0 {
		stagingLimitBytes = 64 * 1024
	}
	return &Importer{svc: svc, StagingLimitBytes: stagingLimitBytes}
}

// stage 把分片解包到临时作业目录，返回副本路径与已打开的句柄。
func stage(srcPath, stagingDir string) (string, *os.File, error) {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return "", nil, fmt.Errorf("breach: 读取分片 %s 失败: %w", srcPath, err)
	}
	tmpPath := filepath.Join(stagingDir, filepath.Base(srcPath)+".staged")
	if werr := os.WriteFile(tmpPath, data, 0o644); werr != nil {
		return "", nil, fmt.Errorf("breach: 解包分片 %s 失败: %w", srcPath, werr)
	}
	f, oerr := os.Open(tmpPath)
	if oerr != nil {
		return "", nil, fmt.Errorf("breach: 打开临时副本 %s 失败: %w", tmpPath, oerr)
	}
	return tmpPath, f, nil
}

// stagingUsage 返回临时作业目录当前占用的字节数。
func stagingUsage(dir string) (int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("breach: 统计作业目录 %s 失败: %w", dir, err)
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		total += info.Size()
	}
	return total, nil
}

// parseShard 从已打开的句柄解析分片内容。
func parseShard(f *os.File) (Shard, error) {
	var sh Shard
	dec := json.NewDecoder(bufio.NewReader(f))
	if err := dec.Decode(&sh); err != nil {
		return Shard{}, fmt.Errorf("breach: 解析分片 %s 失败: %w", f.Name(), err)
	}
	if strings.TrimSpace(sh.ReachCode) == "" {
		return Shard{}, fmt.Errorf("breach: 分片 %s 缺少河段代码", f.Name())
	}
	return sh, nil
}

// ImportShards 依次导入多个分片文件。
//
// 导入过程中任意时刻的作业目录占用都不得超过 Importer.StagingLimitBytes，
// 超限时中止导入并返回 model.ErrStagingFull。
func (im *Importer) ImportShards(paths []string, stagingDir string, at func() time.Time) (ImportReport, error) {
	if len(paths) == 0 {
		return ImportReport{}, fmt.Errorf("breach: 导入分片列表为空")
	}
	if mkErr := os.MkdirAll(stagingDir, 0o755); mkErr != nil {
		return ImportReport{}, fmt.Errorf("breach: 创建作业目录 %s 失败: %w", stagingDir, mkErr)
	}

	rep := ImportReport{}
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)

	for _, path := range sorted {
		shard, err := func() (Shard, error) {
			tmpPath, f, serr := stage(path, stagingDir)
			if serr != nil {
				return Shard{}, serr
			}
			defer os.Remove(tmpPath)
			defer f.Close()
			return parseShard(f)
		}()
		if err != nil {
			return rep, err
		}

		for _, rec := range shard.Records {
			b, berr := im.svc.Report(shard.ReachCode, rec.Location, rec.WidthM, at())
			if berr != nil {
				return rep, berr
			}
			rep.Breaches = append(rep.Breaches, b.ID)
			rep.Records++
		}
		rep.Shards++

		used, uerr := stagingUsage(stagingDir)
		if uerr != nil {
			return rep, uerr
		}
		if used > rep.PeakStagingBytes {
			rep.PeakStagingBytes = used
		}
		if used > im.StagingLimitBytes {
			return rep, fmt.Errorf("%w: 作业目录 %s 已占用 %d 字节，上限 %d 字节",
				model.ErrStagingFull, stagingDir, used, im.StagingLimitBytes)
		}
	}
	return rep, nil
}

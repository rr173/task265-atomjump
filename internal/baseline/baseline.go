// Package baseline 提供统一时间基准与频率单位校正。
//
// 多台原子钟上报的原始频率读数必须换算为相对标称频率的偏差（ppb）才能在统一基准上比较；
// 本包还负责校验频率单位是否与标称频率一致（容差内），以及时间基准标识的解析。
package baseline

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// 基准标识：用于区分频率样本换算所用的参考。
const (
	RefNominal  = "nominal"  // 相对标称频率
	RefUTC      = "utc"      // 相对 UTC 主基准
	RefTAI      = "tai"      // 相对 TAI
	RefInternal = "internal" // 内部基准
)

// 单位校验容差：原始读数偏离标称频率超过该相对比例视为单位不符。
const UnitTolerance = 0.05 // 5%

// Normalize 把原始频率读数换算为相对标称频率的偏差（ppb）。
//
// ppb = (freq - nominal) / nominal * 1e9。若 |freq-nominal|/nominal 超过 UnitTolerance，
// 判定为频率单位不符（如误传 MHz 而非 Hz），返回 ErrUnitMismatch。
func Normalize(freqHz, nominalHz float64) (float64, error) {
	if nominalHz <= 0 {
		return 0, fmt.Errorf("nominal frequency must be positive: %v", nominalHz)
	}
	if freqHz <= 0 {
		return 0, fmt.Errorf("frequency reading must be positive: %v", freqHz)
	}
	rel := math.Abs(freqHz-nominalHz) / nominalHz
	if rel > UnitTolerance {
		return 0, fmt.Errorf("%w: freq=%v nominal=%v rel_dev=%v", ErrUnitMismatch, freqHz, nominalHz, rel)
	}
	ppb := (freqHz - nominalHz) / nominalHz * 1e9
	return ppb, nil
}

// ErrUnitMismatch 频率单位不符错误。
var ErrUnitMismatch = fmt.Errorf("frequency unit mismatch")

// ParseBaseline 解析基准标识，未知基准返回 ErrUnknownBaseline。
func ParseBaseline(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", RefNominal:
		return RefNominal, nil
	case RefUTC:
		return RefUTC, nil
	case RefTAI:
		return RefTAI, nil
	case RefInternal:
		return RefInternal, nil
	}
	return "", fmt.Errorf("%w: %s", ErrUnknownBaseline, s)
}

// ErrUnknownBaseline 未知时间基准错误。
var ErrUnknownBaseline = fmt.Errorf("unknown time baseline")

// AlignWindow 将时间戳对齐到窗口栅格。
//
// 以 epoch 为起点、width 为窗口长度，把 t 归一到所在窗口的起止时间。
func AlignWindow(t time.Time, width time.Duration) (start, end time.Time) {
	secs := t.Unix()
	grid := width.Seconds()
	slot := int64(math.Floor(float64(secs) / grid))
	start = time.Unix(slot*int64(grid), 0).UTC()
	end = start.Add(width)
	return start, end
}

// DefaultWindowWidth 默认窗口宽度（60 秒）。
const DefaultWindowWidth = 60 * time.Second

// JumpThresholdPPB 跳变判定阈值：窗口均值相对前窗口偏移超过该值（ppb）视为跳变。
const JumpThresholdPPB = 0.5

// StdDevThresholdPPB 窗口内部标准差阈值：超过视为不稳定（跳变窗口）。
const StdDevThresholdPPB = 0.2

// MinSamplesPerWindow 窗口最少样本数：不足视为缺口。
const MinSamplesPerWindow = 3

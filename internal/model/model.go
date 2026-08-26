// Package model 定义原子钟频率跳变来源判别服务的核心实体、状态机与错误类型。
//
// 业务域：高精度计时实验室。工程师接收原子钟频率样本、环境读数与多链路比对数据，
// 服务统一时间基准、识别频率跳变段并评分来源候选（环境扰动 / 比对链路 / 设备自身状态切换），
// 最终隔离不可信链路、锁定参考钟并发布不可变诊断快照。
package model

import (
	"errors"
	"fmt"
	"time"
)

// 时钟类型。
const (
	ClockTypeCesium    = "cesium"    // 铯原子钟
	ClockTypeRubidium  = "rubidium"  // 铷原子钟
	ClockTypeHydrogen  = "hydrogen"  // 氢原子钟
	ClockTypeMaser     = "maser"     // 氢脉泽
	ClockTypeQuartz    = "quartz"    // 高稳晶振
	ClockTypeReference = "reference" // 参考钟（UTC 主基准）
)

// ClockStatus 时钟运行状态：采集中 → 待分析 → 需复核 → 确认 → 封存。
type ClockStatus string

const (
	ClockCollecting ClockStatus = "collecting" // 采集中：持续接收样本
	ClockAnalyzing  ClockStatus = "analyzing"  // 待分析：样本已就绪，等待检测
	ClockReview     ClockStatus = "review"     // 需复核：跳变已检出，等待人工裁决
	ClockConfirmed  ClockStatus = "confirmed"  // 确认：来源已确认
	ClockSealed     ClockStatus = "sealed"     // 封存：诊断结束，拒绝修改
)

// 允许的状态流转：当前状态 -> 可达状态集合。
var clockTransitions = map[ClockStatus][]ClockStatus{
	ClockCollecting: {ClockAnalyzing, ClockSealed},
	ClockAnalyzing:  {ClockReview, ClockConfirmed, ClockSealed},
	ClockReview:     {ClockConfirmed, ClockSealed},
	ClockConfirmed:  {ClockSealed},
	ClockSealed:     {}, // 封存后不可再流转
}

// WindowStatus 频率窗口状态：原始 → 稳定 / 跳变 / 缺口 / 排除。
type WindowStatus string

const (
	WindowRaw     WindowStatus = "raw"     // 原始：尚未判定
	WindowStable  WindowStatus = "stable"  // 稳定：均方差在阈值内
	WindowJump    WindowStatus = "jump"    // 跳变：频偏均值发生显著跃迁
	WindowGap     WindowStatus = "gap"     // 缺口：窗口内样本不足
	WindowExcluded WindowStatus = "excluded" // 排除：标记为不可信，不参与判别
)

// CandidateSource 跳变来源候选类别。
type CandidateSource string

const (
	SourceEnvironment CandidateSource = "environment" // 环境相关：温度/湿度/气压扰动
	SourceLink        CandidateSource = "link"        // 链路相关：比对链路异常
	SourceDevice      CandidateSource = "device"      // 设备相关：原子钟自身状态切换
	SourceInsufficient CandidateSource = "insufficient" // 证据不足
	SourceConfirmed   CandidateSource = "confirmed"   // 已确认
)

// LinkStatus 比对链路状态。
type LinkStatus string

const (
	LinkHealthy  LinkStatus = "healthy"  // 健康
	LinkSuspect   LinkStatus = "suspect"   // 可疑：检测到不一致
	LinkIsolated  LinkStatus = "isolated"  // 已隔离：工程师判定不可信
)

// SnapshotStatus 诊断快照状态：草稿 → 发布 → 替代。
type SnapshotStatus string

const (
	SnapshotDraft      SnapshotStatus = "draft"      // 草稿
	SnapshotPublished  SnapshotStatus = "published"  // 已发布（不可变）
	SnapshotSuperseded SnapshotStatus = "superseded" // 已被新快照替代
)

// Clock 一台原子钟的运行记录。
type Clock struct {
	ID           int64       `json:"id"`
	Name         string      `json:"name"`
	ClockType    string      `json:"clock_type"`
	NominalHz    float64     `json:"nominal_hz"`     // 标称频率（Hz）
	Status       ClockStatus `json:"status"`
	ReferenceID  int64       `json:"reference_id"`   // 关联参考钟 ID，0 表示自身为参考钟
	IsReference  bool        `json:"is_reference"`   // 是否为 UTC 主基准
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

// Sample 一条频率样本（相对标称频率的偏差，ppb）。
type Sample struct {
	ID           int64     `json:"id"`
	ClockID      int64     `json:"clock_id"`
	Seq          int64     `json:"seq"`          // 时钟内递增序号（幂等键：clock_id+seq）
	TakenAt      time.Time `json:"taken_at"`     // 采集时刻（统一时间基准）
	FreqHz       float64   `json:"freq_hz"`      // 原始频率读数
	OffsetPPB    float64   `json:"offset_ppb"`   // 相对标称频率偏差（服务换算）
	BaselineRef  string    `json:"baseline_ref"` // 换算所用基准标识
	CreatedAt    time.Time `json:"created_at"`
}

// EnvReading 一条环境读数。
type EnvReading struct {
	ID           int64     `json:"id"`
	ClockID      int64     `json:"clock_id"`
	TakenAt      time.Time `json:"taken_at"`
	TempC        float64   `json:"temp_c"`        // 温度（摄氏度）
	HumidityPct  float64   `json:"humidity_pct"`  // 相对湿度（%）
	PressureHPa  float64   `json:"pressure_hpa"`  // 气压（hPa）
	Vibration    float64   `json:"vibration"`     // 振动烈度（mm/s）
	CreatedAt    time.Time `json:"created_at"`
}

// CompareLink 一条比对链路：被测钟 -> 参考钟。
type CompareLink struct {
	ID          int64      `json:"id"`
	SubjectID   int64      `json:"subject_id"`   // 被测时钟
	ReferenceID int64      `json:"reference_id"` // 参考时钟
	OffsetPPB   float64    `json:"offset_ppb"`   // 当前两钟频差
	Status      LinkStatus `json:"status"`
	LatencyMS   int64      `json:"latency_ms"`   // 比对链路时延
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// FreqWindow 一段固定时长的频率窗口。
type FreqWindow struct {
	ID         int64        `json:"id"`
	ClockID    int64        `json:"clock_id"`
	StartAt    time.Time    `json:"start_at"`
	EndAt      time.Time    `json:"end_at"`
	SampleN    int          `json:"sample_n"`
	MeanPPB    float64      `json:"mean_ppb"`   // 窗口内频偏均值
	StdDevPPB  float64      `json:"stddev_ppb"` // 窗口内频偏标准差
	Status     WindowStatus `json:"status"`
	CreatedAt  time.Time    `json:"created_at"`
}

// JumpSegment 一个跳变段（连续 jump 窗口合并而成）。
type JumpSegment struct {
	ID          int64     `json:"id"`
	ClockID     int64     `json:"clock_id"`
	WindowID    int64     `json:"window_id"`
	StartAt     time.Time `json:"start_at"`
	EndAt       time.Time `json:"end_at"`
	MagnitudePPB float64  `json:"magnitude_ppb"` // 跳变幅度
	Direction   string    `json:"direction"`     // up / down
	DetectedAt  time.Time `json:"detected_at"`
	Status      string    `json:"status"`        // open / confirmed
}

// SourceCandidate 跳变来源候选（含评分）。
type SourceCandidate struct {
	ID        int64           `json:"id"`
	JumpID    int64           `json:"jump_id"`
	Source    CandidateSource `json:"source"`
	Score     float64         `json:"score"`  // 0~1 置信度
	Rationale string          `json:"rationale"` // 判定理由（证据链描述）
	CreatedAt time.Time       `json:"created_at"`
}

// Snapshot 诊断快照（发布后不可变）。
type Snapshot struct {
	ID            int64          `json:"id"`
	ClockID       int64          `json:"clock_id"`
	Status        SnapshotStatus `json:"status"`
	ReferenceID   int64          `json:"reference_id"`  // 锁定的参考钟
	IsolatedLinks []int64        `json:"isolated_links"` // 已隔离链路 ID 集合
	Summary       string         `json:"summary"`       // 诊断结论摘要
	JumpIDs       []int64          `json:"jump_ids"`      // 覆盖的跳变段
	Evidence      *FrozenEvidence  `json:"evidence,omitempty"`
	CreatedAt     time.Time        `json:"created_at"`
	PublishedAt   *time.Time       `json:"published_at,omitempty"`
	SupersededAt  *time.Time       `json:"superseded_at,omitempty"`
}

// 领域错误。
var (
	ErrClockNotFound       = errors.New("clock not found")
	ErrClockSealed         = errors.New("clock is sealed, mutation rejected")
	ErrInvalidTransition   = errors.New("invalid clock status transition")
	ErrSampleDuplicate     = errors.New("duplicate sample seq for clock")
	ErrUnitMismatch        = errors.New("frequency unit mismatch: reading deviates from nominal beyond tolerance")
	ErrUnknownBaseline     = errors.New("unknown time baseline reference")
	ErrReferenceSelfLoop   = errors.New("reference loop: clock cannot compare with itself")
	ErrLinkNotFound        = errors.New("compare link not found")
	ErrWindowNotFound      = errors.New("frequency window not found")
	ErrJumpNotFound        = errors.New("jump segment not found")
	ErrSnapshotNotFound    = errors.New("snapshot not found")
	ErrSnapshotImmutable   = errors.New("published snapshot is immutable")
	ErrSnapshotAlreadyPub  = errors.New("snapshot already published")
	ErrInsufficientSamples = errors.New("insufficient samples to analyze")
	ErrConflict            = errors.New("state conflict")
	ErrCanceled            = errors.New("operation canceled")
)

// FrozenJump 发布瞬间冻结的跳变段证据，与之后 live 表分叉无关。
type FrozenJump struct {
	ID           int64   `json:"id"`
	MagnitudePPB float64 `json:"magnitude_ppb"`
	Direction    string  `json:"direction"`
	Status       string  `json:"status"`
}

// FrozenEvidence 快照发布时的不可变证据：时钟状态、隔离链路、跳变幅度。
type FrozenEvidence struct {
	ClockStatus   ClockStatus  `json:"clock_status"`
	IsolatedLinks []int64      `json:"isolated_links"`
	Jumps         []FrozenJump `json:"jumps"`
}

// CanTransition 校验时钟状态流转是否合法。
func (s ClockStatus) CanTransition(next ClockStatus) bool {
	for _, t := range clockTransitions[s] {
		if t == next {
			return true
		}
	}
	return false
}

// Valid 校验窗口状态值。
func (w WindowStatus) Valid() bool {
	switch w {
	case WindowRaw, WindowStable, WindowJump, WindowGap, WindowExcluded:
		return true
	}
	return false
}

// Valid 校验来源类别值。
func (c CandidateSource) Valid() bool {
	switch c {
	case SourceEnvironment, SourceLink, SourceDevice, SourceInsufficient, SourceConfirmed:
		return true
	}
	return false
}

// Valid 校验链路状态值。
func (l LinkStatus) Valid() bool {
	switch l {
	case LinkHealthy, LinkSuspect, LinkIsolated:
		return true
	}
	return false
}

// ErrClockStatus 构造状态流转错误。
func ErrClockStatus(from, to ClockStatus) error {
	return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
}

// ValidateClockType 校验时钟类型。
func ValidateClockType(t string) error {
	switch t {
	case ClockTypeCesium, ClockTypeRubidium, ClockTypeHydrogen, ClockTypeMaser, ClockTypeQuartz, ClockTypeReference:
		return nil
	}
	return fmt.Errorf("unknown clock type: %s", t)
}

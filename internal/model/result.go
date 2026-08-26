package model

// AnalysisResult 一次分析任务的汇总结果。
type AnalysisResult struct {
	ClockID  int64          `json:"clock_id"`
	Windows  []*FreqWindow  `json:"windows"`
	Jumps    []*JumpSegment `json:"jumps"`
	Messages []string       `json:"messages"`
}

// AttributionResult 一次来源判别的汇总结果。
type AttributionResult struct {
	JumpID     int64              `json:"jump_id"`
	Candidates []*SourceCandidate `json:"candidates"`
	Verdict    CandidateSource    `json:"verdict"` // 最高分候选
	VerdictScore float64          `json:"verdict_score"`
}

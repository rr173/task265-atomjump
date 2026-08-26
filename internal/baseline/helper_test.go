package baseline

import "time"

func timeFromUnix(sec int64) time.Time { return time.Unix(sec, 0).UTC() }

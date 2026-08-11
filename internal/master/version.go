package master

// 可由构建注入：-ldflags "-X biliTickerStorm/internal/master.Version=v1.0.0"
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

package logger

// LogConfig 日志配置
type LogConfig struct {
	Level      string `yaml:"level"`       // debug/info/warn/error
	FilePath   string `yaml:"file_path"`   // 日志文件路径
	MaxSizeMB  int    `yaml:"max_size_mb"` // 单文件最大大小(MB)
	MaxBackups int    `yaml:"max_backups"` // 最大备份数
}

package api

// PathDefaults 是 owning daemon 进程所在机器的 terminal 创建默认值。
type PathDefaults struct {
	DefaultCommand []string
	DefaultCWD     string
}

// PathDirectoryQuery 请求 daemon 文件系统中的目录候选。
type PathDirectoryQuery struct {
	Prefix string
	Limit  int
}

// PathDirectoryEntry 是可写回 prompt 的 daemon-owned 目录候选。
type PathDirectoryEntry struct {
	Name string
	Path string
}

// PathDirectoryResult 是 daemon 文件系统目录候选窗口。
type PathDirectoryResult struct {
	BasePath  string
	Entries   []PathDirectoryEntry
	Missing   bool
	Truncated bool
}

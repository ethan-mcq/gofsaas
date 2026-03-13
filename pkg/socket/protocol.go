package socket

// Request is the JSON message sent by clients to the socket server.
type Request struct {
	Op   string `json:"op"`
	Path string `json:"path"`
	Wait bool   `json:"wait,omitempty"`
}

// Response is the JSON message returned by the socket server.
type Response struct {
	// exists
	Exists    bool  `json:"exists,omitempty"`
	Cached    bool  `json:"cached,omitempty"`
	SizeBytes int64 `json:"size_bytes,omitempty"`
	// fetch / clean
	OK         bool   `json:"ok,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
	FromCache  bool   `json:"from_cache,omitempty"`
	FreedBytes int64  `json:"freed_bytes,omitempty"`
	// status
	CacheBytesUsed int64 `json:"cache_bytes_used,omitempty"`
	FilesCached    int   `json:"files_cached,omitempty"`
	FilesFetching  int   `json:"files_fetching,omitempty"`
}

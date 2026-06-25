package protocol

const Version = "2.1"

// DeviceInfo is broadcast via UDP and HTTP register.
type DeviceInfo struct {
	Alias       string `json:"alias"`
	Version     string `json:"version"`
	DeviceModel string `json:"deviceModel,omitempty"`
	DeviceType  string `json:"deviceType"`
	Fingerprint string `json:"fingerprint"`
	Port        int    `json:"port"`
	Protocol    string `json:"protocol"`
	Download    bool   `json:"download"`
	Announce    bool   `json:"announce,omitempty"`
}

// FileInfo is sent per-file in a PrepareUploadRequest.
type FileInfo struct {
	ID       string `json:"id"`
	FileName string `json:"fileName"`
	Size     int64  `json:"size"`
	FileType string `json:"fileType,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
	Preview  string `json:"preview,omitempty"`
}

// PrepareUploadRequest is the body of POST /api/localsend/v2/prepare-upload.
type PrepareUploadRequest struct {
	Info  DeviceInfo          `json:"info"`
	Files map[string]FileInfo `json:"files"`
}

// PrepareUploadResponse is returned by the receiver on acceptance.
type PrepareUploadResponse struct {
	SessionID string            `json:"sessionId"`
	Files     map[string]string `json:"files"` // fileID -> token
}

// DiscoveredDevice combines DeviceInfo with its network address.
type DiscoveredDevice struct {
	DeviceInfo
	IP       string `json:"ip"`
	Favorite bool   `json:"favorite"`
}

package pasadatos

import "time"

const (
	AppName    = "pasaDATOS"
	AppVersion = "1.1.1"

	StatusPending   = "pending"
	StatusUploading = "uploading"
	StatusReady     = "ready"
	StatusDelivered = "delivered"
	StatusCancelled = "cancelled"
	StatusError     = "error"
)

type Device struct {
	ID         string    `json:"id"`
	TokenHash  string    `json:"token_hash"`
	Name       string    `json:"name"`
	Platform   string    `json:"platform"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

type Link struct {
	ID        string    `json:"id"`
	DeviceA   string    `json:"device_a"`
	DeviceB   string    `json:"device_b"`
	CreatedAt time.Time `json:"created_at"`
}

type Pairing struct {
	ID              string     `json:"id"`
	Code            string     `json:"code"`
	CreatorDeviceID string     `json:"creator_device_id"`
	CreatedAt       time.Time  `json:"created_at"`
	ExpiresAt       time.Time  `json:"expires_at"`
	UsedAt          *time.Time `json:"used_at,omitempty"`
	UsedByDeviceID  string     `json:"used_by_device_id,omitempty"`
}

type Transfer struct {
	ID                string     `json:"id"`
	SenderDeviceID    string     `json:"sender_device_id"`
	ReceiverDeviceID  string     `json:"receiver_device_id"`
	Filename          string     `json:"filename"`
	Size              int64      `json:"size"`
	MIME              string     `json:"mime,omitempty"`
	SHA256            string     `json:"sha256,omitempty"`
	Status            string     `json:"status"`
	StorageKind       string     `json:"storage_kind"` // spool | linked
	StoredPath        string     `json:"stored_path,omitempty"`
	LocalSourcePath   string     `json:"local_source_path,omitempty"`
	SourceLabel       string     `json:"source_label,omitempty"`
	DestinationLabel  string     `json:"destination_label,omitempty"`
	UploadedBytes     int64      `json:"uploaded_bytes"`
	DownloadedBytes   int64      `json:"downloaded_bytes"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	ExpiresAt         time.Time  `json:"expires_at"`
	DeliveredAt       *time.Time `json:"delivered_at,omitempty"`
	Error             string     `json:"error,omitempty"`
	DeleteAfter       *time.Time `json:"delete_after,omitempty"`
	SenderDisplayName string     `json:"sender_display_name,omitempty"`
	ReceiverName      string     `json:"receiver_name,omitempty"`
}

type DownloadToken struct {
	TokenHash        string    `json:"token_hash"`
	TransferID       string    `json:"transfer_id"`
	ReceiverDeviceID string    `json:"receiver_device_id"`
	CreatedAt        time.Time `json:"created_at"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type PersistentState struct {
	Version        int                      `json:"version"`
	Devices        map[string]Device        `json:"devices"`
	Links          map[string]Link          `json:"links"`
	Pairings       map[string]Pairing       `json:"pairings"`
	Transfers      map[string]Transfer      `json:"transfers"`
	DownloadTokens map[string]DownloadToken `json:"download_tokens"`
}

type DesktopConfig struct {
	Version          int       `json:"version"`
	DeviceID         string    `json:"device_id"`
	DeviceToken      string    `json:"device_token"`
	DeviceName       string    `json:"device_name"`
	NativeToken      string    `json:"native_token"`
	ReceiveFolder    string    `json:"receive_folder"`
	RemoteServerURL  string    `json:"remote_server_url"`
	ActiveMode       string    `json:"active_mode"`
	ListenAddress    string    `json:"listen_address"`
	AutoStart        bool      `json:"auto_start"`
	AutoDownload     bool      `json:"auto_download"`
	LocalFileTTLHour int       `json:"local_file_ttl_hours"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type HistoryItem struct {
	ID              string     `json:"id"`
	TransferID      string     `json:"transfer_id,omitempty"`
	Direction       string     `json:"direction"`
	Mode            string     `json:"mode"`
	PeerID          string     `json:"peer_id,omitempty"`
	PeerName        string     `json:"peer_name,omitempty"`
	Filename        string     `json:"filename"`
	Size            int64      `json:"size"`
	Status          string     `json:"status"`
	Progress        int64      `json:"progress"`
	SourcePath      string     `json:"source_path,omitempty"`
	DestinationPath string     `json:"destination_path,omitempty"`
	StartedAt       time.Time  `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	Error           string     `json:"error,omitempty"`
}

type HistoryData struct {
	Version int           `json:"version"`
	Items   []HistoryItem `json:"items"`
}

type TransferJob struct {
	ID              string     `json:"id"`
	TransferID      string     `json:"transfer_id,omitempty"`
	Direction       string     `json:"direction"`
	Mode            string     `json:"mode"`
	PeerID          string     `json:"peer_id,omitempty"`
	PeerName        string     `json:"peer_name,omitempty"`
	Filename        string     `json:"filename"`
	Size            int64      `json:"size"`
	Done            int64      `json:"done"`
	Status          string     `json:"status"`
	SourcePath      string     `json:"source_path,omitempty"`
	DestinationPath string     `json:"destination_path,omitempty"`
	StartedAt       time.Time  `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	Error           string     `json:"error,omitempty"`
}

type PeerView struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Platform   string    `json:"platform"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Online     bool      `json:"online"`
}

type ModeState struct {
	Mode          string     `json:"mode"`
	Configured    bool       `json:"configured"`
	Online        bool       `json:"online"`
	ServerURL     string     `json:"server_url"`
	DisplayURL    string     `json:"display_url"`
	Peers         []PeerView `json:"peers"`
	PairingCode   string     `json:"pairing_code,omitempty"`
	PairingExpiry *time.Time `json:"pairing_expiry,omitempty"`
	JoinURL       string     `json:"join_url,omitempty"`
	Error         string     `json:"error,omitempty"`
}

type DesktopState struct {
	AppName       string         `json:"app_name"`
	Version       string         `json:"version"`
	DeviceID      string         `json:"device_id"`
	DeviceName    string         `json:"device_name"`
	ReceiveFolder string         `json:"receive_folder"`
	ActiveMode    string         `json:"active_mode"`
	AutoStart     bool           `json:"auto_start"`
	AutoDownload  bool           `json:"auto_download"`
	Local         ModeState      `json:"local"`
	Remote        ModeState      `json:"remote"`
	Jobs          []TransferJob  `json:"jobs"`
	History       []HistoryItem  `json:"history"`
	SelectedFiles []SelectedFile `json:"selected_files,omitempty"`
	Now           time.Time      `json:"now"`
}

type SelectedFile struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type ServerOptions struct {
	ListenAddress          string
	DataDir                string
	PublicURL              string
	FileTTL                time.Duration
	MetadataTTL            time.Duration
	PairingTTL             time.Duration
	DownloadTokenTTL       time.Duration
	DeliveredDeleteDelay   time.Duration
	MaxFileBytes           int64
	DesktopMode            bool
	NativeToken            string
	DesktopDeviceID        string
	DesktopReceiveCallback func(string)
	DesktopExitCallback    func()
	Logger                 Logger
}

type Logger interface {
	Printf(format string, v ...any)
}

package main

import "git.uozi.org/uozi/potato-api/model"

// 设备上报 ota 服务器
type DeviceInfoRequest struct {
	Version             int         `json:"version"`
	Language            string      `json:"language"`
	FlashSize           int         `json:"flash_size"`
	MinimumFreeHeapSize int         `json:"minimum_free_heap_size"`
	MacAddress          string      `json:"mac_address"`
	UUID                string      `json:"uuid"`
	ChipModelName       string      `json:"chip_model_name"`
	ChipInfo            ChipInfo    `json:"chip_info"`
	Application         AppInfo     `json:"application"`
	PartitionTable      []Partition `json:"partition_table"`
	OTA                 OTAInfo     `json:"ota"`
	Board               BoardInfo   `json:"board"`
}

type ChipInfo struct {
	Model    int `json:"model"`
	Cores    int `json:"cores"`
	Revision int `json:"revision"`
	Features int `json:"features"`
}

type AppInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	CompileTime string `json:"compile_time"`
	IDFVersion  string `json:"idf_version"`
	ELFSHA256   string `json:"elf_sha256"`
}

type Partition struct {
	Label   string `json:"label"`
	Type    int    `json:"type"`
	Subtype int    `json:"subtype"`
	Address int    `json:"address"`
	Size    int    `json:"size"`
}

type OTAInfo struct {
	Label string `json:"label"`
}

type BoardInfo struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Revision string `json:"revision"`
	Carrier  string `json:"carrier"`
	CSQ      string `json:"csq"`
	IMEI     string `json:"imei"`
	ICCID    string `json:"iccid"`
	Cereg    Cereg  `json:"cereg"`
}

type Cereg struct {
	Stat int    `json:"stat"`
	Tac  string `json:"tac"`
	Ci   string `json:"ci"`
	AcT  int    `json:"AcT"`
}

// 服务器返回服务器信息
type MQTTConfig struct {
	Endpoint       string `json:"endpoint"`
	ClientID       string `json:"client_id"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	PublishTopic   string `json:"publish_topic"`
	SubscribeTopic string `json:"subscribe_topic"`
}

type WebSocketConfig struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

type ServerTime struct {
	Timestamp      int64 `json:"timestamp"`
	TimezoneOffset int   `json:"timezone_offset"`
}

type FirmwareConfig struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

// AuthRequest 设备认证请求
type AuthRequest struct {
	MAC string `json:"mac" binding:"required"`
}

// AuthResponse 设备认证响应
type AuthResponse struct {
	MQTT       MQTTConfig      `json:"mqtt"`
	WebSocket  WebSocketConfig `json:"websocket"`
	ServerTime ServerTime      `json:"server_time"`
	Firmware   FirmwareConfig  `json:"firmware"`
}

type MqttMessagePayload struct {
	Type        string       `json:"type"`
	Version     int          `json:"version,omitempty"`
	Transport   string       `json:"transport,omitempty"`
	AudioParams *AudioParams `json:"audio_params,omitempty"`
	UDP         UDPConfig    `json:"udp"`
	SessionID   string       `json:"session_id,omitempty"`
	Nonce       string       `json:"nonce,omitempty"`

	DeviceInfo *DeviceInfoRequest `json:"device_info,omitempty"`
	State      string             `json:"state,omitempty"`
	Mode       string             `json:"mode,omitempty"`
	Reason     string             `json:"reason,omitempty"`
	Text       string             `json:"text,omitempty"`
	Emotion    string             `json:"emotion,omitempty"`
	Commands   []*Command         `json:"commands,omitempty"`
	Time       int64              `json:"time,omitempty"`
	OTA        *model.OTA         `json:"ota,omitempty"`
	States     []*States          `json:"states,omitempty"`
	Update     bool               `json:"update"`
	StatusCode int                `json:"status_code"`
}

type AudioParams struct {
	Format        string `json:"format,omitempty"`
	SampleRate    int    `json:"sample_rate,omitempty"`
	Channels      int    `json:"channels,omitempty"`
	FrameDuration int    `json:"frame_duration,omitempty"`
}

type Command struct {
	Name       string                 `json:"name"`       // 命令名称（如 "Speaker"）
	Method     string                 `json:"method"`     // 方法名（如 "SetVolume"）
	Parameters map[string]interface{} `json:"parameters"` // 原始参数（可后续解析）
	// 或使用具体结构体：
	// Parameters VolumeParameters `json:"parameters"`
}

type States struct {
	Name  string                 `json:"name"`
	State map[string]interface{} `json:"state"` // 修改此处
}

type UDPConfig struct {
	Server     string `json:"server"`
	Port       int    `json:"port"`
	Encryption string `json:"encryption"`
	Key        string `json:"key"`
	Nonce      string `json:"nonce"`
}

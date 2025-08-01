package main

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"math/rand"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-audio/wav"
	"github.com/oov/audio/resampler"
	"github.com/spf13/cast"
	"github.com/uozi-tech/cosy/logger"
	"layeh.com/gopus"
)

/*

uint64=1754025095011
 int64=1754025095055
wait  uint64=2139


uint64=1754025095376
 int64=1754025095403

*/
// 并发测试配置
const (
	// 并发测试用例数量
	ConcurrentTestCount = 2

	// 服务器配置
	ServerHost = "aiotcomm.com.cn"
	ServerPort = 50202
	MQTTHost   = "10.14.2.54"
	MQTTPort   = 1883
	UDPHost    = "2huo.tech"
	UDPPort    = 50400

	// 默认设备配置
	DefaultDeviceMAC  = "10:51:db:72:70:a8"
	DefaultDeviceUUID = "3c05ed7d-eae5-41ec-aebf-c284c9ddce90"

	// 测试文件配置
	TestWavFile    = "./天问一号.wav"
	TestDuration   = 5 * time.Second
	SampleInterval = 100 * time.Millisecond

	// 设备配置文件路径
	DeviceConfigFile = "statics/test_json/device_config.json"

	// 音频文件目录
	AudioFilesDir = "./statics/audio/"
)

// 设备配置结构
type DeviceConfig struct {
	Devices []DeviceInfo `json:"devices"`
}

// 测试结果统计
type TestResult struct {
	DeviceMAC    string        `json:"device_mac"`
	SessionID    string        `json:"session_id"`
	StartTime    time.Time     `json:"start_time"`
	EndTime      time.Time     `json:"end_time"`
	Duration     time.Duration `json:"duration"`
	AuthTime     time.Duration `json:"auth_time"`
	HelloTime    time.Duration `json:"hello_time"`
	IoTTime      time.Duration `json:"iot_time"`
	ListenTime   time.Duration `json:"listen_time"`
	AudioTime    time.Duration `json:"audio_time"`
	TextTime     time.Duration `json:"text_time"`
	GoodbyeTime  time.Duration `json:"goodbye_time"`
	AudioPackets int           `json:"audio_packets"`
	TextMessages int           `json:"text_messages"`
	Success      bool          `json:"success"`
	ErrorMessage string        `json:"error_message"`
}

// 全局测试结果收集器
var (
	testResults     []TestResult
	testResultsLock sync.Mutex
)

// 测试状态
type TestState struct {
	SessionID         string
	Nonce             [16]byte
	MQTTClient        mqtt.Client
	MQTT              MQTT
	UDP               UDP
	UDPConn           *net.UDPConn
	CommonPushTopic   string
	SubServerTopic    string
	Sequence          uint32
	ReceivedText      []string
	ReceivedAudio     []int // 接收到的音频包数量
	AcceptAudioPacket *AudioPacket
	// 每个设备独立的音频数据存储
	AudioFrames      [][]byte
	AudioFramesMutex sync.Mutex
	// 时间记录
	ListenStopTime     time.Time
	FirstAudioTime     time.Time
	FirstAudioReceived bool
}
type UDP struct {
	Server string `json:"server"`
	Port   int
}
type MQTT struct {
	EndPoint        string
	Username        string
	Password        string
	ClientID        string
	CommonPushTopic string
}
type AudioPacket struct {
	Nonce     [16]byte // 16字节nonce
	AudioData []byte   // 音频数据
	Size      uint32   // 音频数据大小
	SessionID string   // 会话ID
	Timestamp uint64   // 时间戳
	Sequence  uint32   // 序列号
}

const (
	WaitForGoodbyeTime = 1  // 等待goodbye响应的时间（秒）
	WaitForHelloTime   = 1  // 等待hello响应的时间（秒）
	WaitForEndTime     = 20 // 等待文本消息响应的时间（秒）
)

// func main() {
// 	for i := 0; i < 10000; i++ {
// 		mac := fmt.Sprintf("bb:51:db:72:70:%02x", i)
// 		hashMAC := HashMAC(mac)
// 		logger.Debugf("hashMAC: %v", hashMAC)
// 		logger.Debugf("mac: %v", mac)
// 	}
// }

// 集成测试主函数
// func TestIntegration(t *testing.T) {
func main() {
	logger.Info("开始并发集成测试...")

	// 初始化随机数种子
	rand.Seed(time.Now().UnixNano())

	// 生成设备配置文件
	if err := generateDeviceConfig(); err != nil {
		logger.Errorf("生成设备配置文件失败: %v", err)
		os.Exit(1)
	}

	// 读取设备配置
	devices, err := loadDeviceConfig()
	if err != nil {
		logger.Errorf("读取设备配置失败: %v", err)
		os.Exit(1)
	}

	logger.Infof("准备测试 %d 个设备", len(devices))

	// 启动并发测试
	var wg sync.WaitGroup
	startTime := time.Now()

	for i, device := range devices {
		wg.Add(1)
		logger.Infof("启动设备 %d 测试: %s", i+1, device.MacAddress)
		go runSingleDeviceTest(device, &wg)

		// 稍微延迟启动，避免同时连接过多
		time.Sleep(100 * time.Millisecond)
	}

	// 等待所有测试完成
	wg.Wait()

	totalTime := time.Since(startTime)
	logger.Infof("所有设备测试完成，总耗时: %v", totalTime)

	// 打印测试结果统计
	printTestResults()

	logger.Info("并发集成测试完成")
}

func testMQQTTListenStop(state *TestState) error {
	logger.Info("发送MQTT listen stop消息...")

	listenMsg := map[string]interface{}{
		"session_id": state.SessionID,
		"type":       "listen",
		"state":      "stop",
	}

	listenData, _ := json.Marshal(listenMsg)
	if token := state.MQTTClient.Publish(state.CommonPushTopic, 1, false, listenData); token.Wait() && token.Error() != nil {
		return fmt.Errorf("发布listen stop消息失败: %v", token.Error())
	}

	logger.Info("MQTT listen stop消息发送成功")
	return nil
}

func testMQQTTListenStart(state *TestState) error {
	logger.Info("发送MQTT listen start消息...")

	listenMsg := map[string]interface{}{
		"session_id": state.SessionID,
		"type":       "listen",
		"mode":       "manual",

		"state": "start",
	}

	listenData, _ := json.Marshal(listenMsg)
	if token := state.MQTTClient.Publish(state.CommonPushTopic, 1, false, listenData); token.Wait() && token.Error() != nil {
		return fmt.Errorf("发布listen start消息失败: %v", token.Error())
	}

	logger.Info("MQTT listen start消息发送成功")
	return nil
}

var count = 0

func testUDPClient(state *TestState) error {
	logger.Info("启动UDP客户端监听，等待服务端音频响应...")

	// 接收音频数据
	buf := make([]byte, 4096)
	timeoutCount := 0
	successCount := 0

	for {
		// 每次读取前重新设置超时时间为5秒
		err := state.UDPConn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if err != nil {
			return fmt.Errorf("设置UDP读取超时失败: %v", err)
		}

		n, _, err := state.UDPConn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				timeoutCount++
				if timeoutCount%10 == 0 { // 每10次超时打印一次日志
					logger.Debugf("UDP读取数据超时，继续等待... (超时次数: %d, 成功次数: %d)", timeoutCount, successCount)
				}
				continue // 超时继续循环
			}
			logger.Errorf("UDP读取数据失败: %v", err)
			continue
		}

		// 将接收到的音频数据添加到缓冲区
		if n > 0 {
			successCount++
			state.handleAudioPacket(buf[:n], nil)
			// count++
			// logger.Debugf("成功接收音频包 count: %d, 数据大小: %d bytes, 时间戳: %d", count, n, time.Now().UnixMilli())
		}
	}
}

func (s *TestState) handleAudioPacket(data []byte, clientAddr *net.UDPAddr) {
	// 解析音频包
	packet, err := s.parseAudioPacket(data)
	if err != nil {
		logger.Errorf("解析音频包失败: %v", err)
		return
	}

	// 从nonce中提取sessionID
	sessionID := s.extractSessionID(packet.Nonce)
	if sessionID == "" {
		logger.Errorf("无法从nonce中提取sessionID")
		return
	}
	if packet.Sequence <= s.AcceptAudioPacket.Sequence {
		logger.Debugf("收到重复或过期的音频包 [%s], 序列号: %d, 期望: %d", sessionID, packet.Sequence, s.AcceptAudioPacket.Sequence+1)
		return
	}
	s.AcceptAudioPacket.Sequence = packet.Sequence

	// 记录第一个音频包的时间
	if !s.FirstAudioReceived {
		s.FirstAudioTime = time.Now()
		s.FirstAudioReceived = true

		// 计算时间差
		if !s.ListenStopTime.IsZero() {
			timeDiff := s.FirstAudioTime.Sub(s.ListenStopTime)
			logger.Infof("设备 %s 收到第一个音频包，时间差: %v (listen stop -> 第一个音频包)", s.SessionID, timeDiff)
		} else {
			logger.Infof("设备 %s 收到第一个音频包，时间: %v (未记录listen stop时间)", s.SessionID, s.FirstAudioTime)
		}
	}

	// 使用设备自己的音频存储，避免并发冲突
	s.AudioFramesMutex.Lock()
	// 深拷贝音频数据，避免引用问题
	audioDataCopy := make([]byte, len(packet.AudioData))
	copy(audioDataCopy, packet.AudioData)

	s.AudioFrames = append(s.AudioFrames, audioDataCopy)
	// packetCount := len(s.AudioFrames)
	s.AudioFramesMutex.Unlock()

	// logger.Debugf("设备 %s 收到音频数据包，大小: %d bytes，累计: %d 包", s.SessionID, len(packet.AudioData), packetCount)
}

func (s *TestState) extractSessionID(nonce [16]byte) string {
	// nonce格式: 0010 + size(2字节) + sessionID前5字节 + timestamp(4字节) + 自增序列号(3字节)
	// sessionID位于第4-8字节 (从0开始计数)

	// 跳过固定开头(2) + size(2) = 4字节
	sessionIDBytes := nonce[4:9]

	// 转换为16进制字符串
	return hex.EncodeToString(sessionIDBytes)
}
func (s *TestState) parseAudioPacket(data []byte) (*AudioPacket, error) {
	// 正确格式: [16字节nonce][音频数据]
	minLen := 16
	if len(data) < minLen {
		return nil, fmt.Errorf("音频包长度不足: %d < %d", len(data), minLen)
	}

	packet := &AudioPacket{}

	// 1. 解析nonce (16字节)
	copy(packet.Nonce[:], data[0:16])

	// 2. 从nonce中解析各个字段
	size, timestamp, sessionID, sequence := s.parseNonce(packet.Nonce)

	packet.Size = size
	packet.Timestamp = uint64(timestamp)
	packet.SessionID = sessionID
	packet.Sequence = sequence
	// logger.Debug("0100", packet.Size, packet.SessionID, packet.Timestamp, packet.Sequence)

	// 3. 音频数据
	packet.AudioData = data[16:]

	return packet, nil
}
func (s *TestState) parseNonce(nonce [16]byte) (size uint32, timestamp uint32, sessionID string, sequence uint32) {
	pos := 0

	// 1. 跳过固定开头 0010 (2字节)
	pos += 2

	// 2. 解析size (2字节)
	size = uint32(binary.BigEndian.Uint16(nonce[pos : pos+2]))
	pos += 2

	// 3. 解析sessionID (5字节)
	sessionIDBytes := nonce[pos : pos+5]
	sessionID = hex.EncodeToString(sessionIDBytes)
	pos += 5

	// 4. 解析timestamp (4字节)
	timestamp = binary.BigEndian.Uint32(nonce[pos : pos+4])
	pos += 4

	// 5. 解析sequence (3字节)
	sequence = uint32(nonce[pos])<<16 | uint32(nonce[pos+1])<<8 | uint32(nonce[pos+2])

	return
}

// HTTP认证测试
func testHTTPAuth(state *TestState) (AuthResponse, error) {
	logger.Info("1. 开始HTTP认证...")

	// 构建认证请求
	authReq := DeviceInfo{
		Version:             "2",
		Language:            "zh-CN",
		FlashSize:           16777216,
		MinimumFreeHeapSize: 8441000,
		MacAddress:          DefaultDeviceMAC,
		UUID:                DefaultDeviceUUID,
		ChipModelName:       "esp32s3",
		Application: AppInfo{
			Name:        "xiaozhi",
			Version:     "2.1.0",
			CompileTime: "Jul 15 2025T15:28:46Z",
			IDFVersion:  "v5.3.1-dirty",
		},
		Board: BoardInfo{
			Type:     "tudouzi",
			Name:     "tudouzi",
			Revision: "ML307R-DL-MBRH0S01",
			Carrier:  "CHINA MOBILE",
			CSQ:      "26",
			IMEI:     "460240429759840",
			ICCID:    "89860842102481934840",
		},
	}

	// 发送HTTP请求
	jsonData, err := json.Marshal(authReq)
	if err != nil {
		return AuthResponse{}, fmt.Errorf("序列化认证请求失败: %v", err)
	}

	resp, err := http.Post(
		fmt.Sprintf("http://%s:%d/devices/auth", ServerHost, ServerPort),
		"application/json",
		strings.NewReader(string(jsonData)),
	)
	if err != nil {
		return AuthResponse{}, fmt.Errorf("HTTP请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return AuthResponse{}, fmt.Errorf("HTTP认证失败，状态码: %d", resp.StatusCode)
	}

	// 解析响应
	var authResp AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return AuthResponse{}, fmt.Errorf("解析认证响应失败: %v", err)
	}

	logger.Infof("HTTP认证成功: %s", authResp)

	state.CommonPushTopic = authResp.MQTT.PublishTopic

	state.MQTT.EndPoint = authResp.MQTT.Endpoint
	state.MQTT.Username = authResp.MQTT.Username
	state.MQTT.Password = authResp.MQTT.Password
	state.MQTT.ClientID = authResp.MQTT.ClientID
	state.MQTT.CommonPushTopic = authResp.MQTT.PublishTopic

	return authResp, nil
}

// MQTT hello握手测试
func testMQTTHello(authResp AuthResponse, state *TestState) (string, error) {
	logger.Info("2. 开始MQTT hello握手...")

	// 生成sessionID
	state.SessionID = generateSessionID()

	// 生成nonce
	generateNonce(state)

	// 构建hello消息
	helloMsg := map[string]interface{}{
		"type":      "hello",
		"version":   3,
		"transport": "udp",
		"audio_params": map[string]interface{}{
			"format":         "opus",
			"sample_rate":    16000,
			"channels":       1,
			"frame_duration": 60,
		},
		"device_info": map[string]interface{}{
			"mac": DefaultDeviceMAC,
		},
	}

	// 连接MQTT
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s", state.MQTT.EndPoint))

	// 为每个设备生成唯一的客户端ID，避免连接冲突
	uniqueClientID := fmt.Sprintf("%s_%s_%d", state.MQTT.ClientID, DefaultDeviceMAC, time.Now().UnixNano())
	opts.SetClientID(uniqueClientID)

	opts.SetUsername(state.MQTT.Username)
	opts.SetPassword(state.MQTT.Password)
	opts.SetResumeSubs(true)
	opts.SetCleanSession(false)
	opts.SetKeepAlive(30 * time.Second)
	opts.SetPingTimeout(10 * time.Second)

	state.MQTTClient = mqtt.NewClient(opts)
	if token := state.MQTTClient.Connect(); token.Wait() && token.Error() != nil {
		return "", fmt.Errorf("MQTT连接失败: %v", token.Error())
	}

	// 订阅公共主题
	logger.Info("设备订阅自己的主题: ", GetServerTopic(DefaultDeviceMAC))
	if token := state.MQTTClient.Subscribe(GetServerTopic(DefaultDeviceMAC), 1, func(client mqtt.Client, msg mqtt.Message) {
		handleHelloResponse(state, msg)
	}); token.Wait() && token.Error() != nil {
		return "", fmt.Errorf("MQTT订阅失败: %v", token.Error())
	}

	// 发送hello消息
	logger.Info("发送hello消息...到公共主题：", authResp.MQTT.SubscribeTopic)
	helloData, _ := json.Marshal(helloMsg)
	if token := state.MQTTClient.Publish(state.CommonPushTopic, 1, false, helloData); token.Wait() && token.Error() != nil {
		return "", fmt.Errorf("MQTT发布hello失败: %v", token.Error())
	}

	// 等待hello响应
	time.Sleep(WaitForHelloTime * time.Second)

	if state.SessionID == "" {
		return "", fmt.Errorf("未收到hello响应")
	}

	logger.Infof("MQTT hello握手成功，SessionID: %s", state.SessionID)
	return state.SessionID, nil
}

// 处理hello响应
func handleHelloResponse(state *TestState, msg mqtt.Message) {
	var response MqttMessagePayload
	if err := json.Unmarshal(msg.Payload(), &response); err != nil {
		logger.Errorf("解析hello响应失败: %v", err)
		return
	}

	switch response.Type {
	case "hello":
		state.SessionID = response.SessionID
		state.UDP.Server = response.UDP.Server
		state.UDP.Port = response.UDP.Port
		logger.Infof("收到hello响应，SessionID: %s,topic: %s,udp: %s:%d", state.SessionID, state.CommonPushTopic, state.UDP.Server, state.UDP.Port)
	case "end_chat":
		logger.Debug("收到end_chat响应")

	}
}

// MQTT IoT消息测试
func testMQTTIoT(state *TestState) error {
	logger.Info("3. 发送MQTT IoT消息...")

	// 检查MQTT连接状态
	if !state.MQTTClient.IsConnected() {
		return fmt.Errorf("MQTT客户端未连接")
	}

	logger.Infof("订阅服务端主题: %s", state.SubServerTopic)

	// 订阅服务端主题
	if token := state.MQTTClient.Subscribe(state.SubServerTopic, 1, func(client mqtt.Client, msg mqtt.Message) {
		handleServerMessage(state, msg)
	}); token.Wait() && token.Error() != nil {
		return fmt.Errorf("订阅服务端主题失败: %v", token.Error())
	}

	// 发送IoT消息
	iotMsg := map[string]interface{}{
		"session_id": state.SessionID,
		"type":       "iot",
		"update":     true,
		"descriptors": []map[string]interface{}{
			{
				"name":        "Speaker",
				"description": "扬声器",
				"properties": map[string]interface{}{
					"volume": map[string]interface{}{
						"description": "当前音量值",
						"type":        "number",
					},
				},
				"methods": map[string]interface{}{
					"SetVolume": map[string]interface{}{
						"description": "设置音量",
						"parameters": map[string]interface{}{
							"volume": map[string]interface{}{
								"description": "0到100之间的整数",
								"type":        "number",
							},
						},
					},
				},
			},
		},
	}

	iotData, _ := json.Marshal(iotMsg)
	if token := state.MQTTClient.Publish(state.CommonPushTopic, 1, false, iotData); token.Wait() && token.Error() != nil {
		return fmt.Errorf("发布IoT消息失败: %v", token.Error())
	}

	// 发送设备状态
	stateMsg := map[string]interface{}{
		"session_id": state.SessionID,
		"type":       "iot",
		"update":     true,
		"states": []map[string]interface{}{
			{
				"name": "Speaker",
				"state": map[string]interface{}{
					"volume": 90,
				},
			},
			{
				"name": "Battery",
				"state": map[string]interface{}{
					"level":    80,
					"charging": false,
				},
			},
		},
	}

	stateData, _ := json.Marshal(stateMsg)
	if token := state.MQTTClient.Publish(state.CommonPushTopic, 1, false, stateData); token.Wait() && token.Error() != nil {
		return fmt.Errorf("发布设备状态失败: %v", token.Error())
	}

	logger.Info("MQTT IoT消息发送成功")
	return nil
}

// 建立UDP连接
func setupUDPConnection(state *TestState) error {
	logger.Info("建立UDP连接...")
	logger.Debugf("UDP地址: %s:%d", state.UDP.Server, state.UDP.Port)
	// 连接UDP服务器
	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", state.UDP.Server, state.UDP.Port))
	if err != nil {
		return fmt.Errorf("解析UDP地址失败: %v", err)
	}

	state.UDPConn, err = net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return fmt.Errorf("连接UDP服务器失败: %v", err)
	}

	logger.Info("UDP连接建立成功")
	return nil
}

// MQTT listen消息测试
func testMQTTListen(state *TestState) error {
	logger.Info("4. 发送MQTT listen消息...")

	listenMsg := map[string]interface{}{
		"session_id": state.SessionID,
		"type":       "listen",
		"state":      "start",
		"mode":       "manual",
	}

	listenData, _ := json.Marshal(listenMsg)
	if token := state.MQTTClient.Publish(state.CommonPushTopic, 1, false, listenData); token.Wait() && token.Error() != nil {
		return fmt.Errorf("发布listen消息失败: %v", token.Error())
	}

	logger.Info("MQTT listen消息发送成功")
	return nil
}

// UDP音频传输测试
func testUDPAudio(state *TestState) error {
	logger.Info("5. 开始UDP音频传输...")

	// 连接UDP服务器
	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", state.UDP.Server, state.UDP.Port))
	if err != nil {
		return fmt.Errorf("解析UDP地址失败: %v", err)
	}

	state.UDPConn, err = net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return fmt.Errorf("连接UDP服务器失败: %v", err)
	}

	// 检查测试音频文件
	if _, err := os.Stat(TestWavFile); os.IsNotExist(err) {
		logger.Warnf("测试音频文件不存在: %s，跳过音频传输测试", TestWavFile)
		return nil
	}

	// 转换WAV到Opus
	opusFrames, err := ConvertWavToOpus(TestWavFile, AudioConfig{
		SampleRate:    16000,
		Channels:      1,
		FrameDuration: 60,
	})
	if err != nil {
		return fmt.Errorf("转换WAV到Opus失败: %v", err)
	}

	logger.Infof("开始发送 %d 个音频帧", len(opusFrames))

	// 发送音频帧
	for i, frame := range opusFrames {
		// 构建音频包
		audioPacket, err := state.buildAudioPacket(frame)
		if err != nil {
			return fmt.Errorf("构建音频包失败: %v", err)
		}

		// 发送音频包
		_, err = state.UDPConn.Write(audioPacket)
		if err != nil {
			return fmt.Errorf("发送音频包失败: %v", err)
		}

		logger.Debugf("发送音频包 %d/%d，大小: %d bytes,sessionID: %s, 时间戳: %d", i+1, len(opusFrames), len(audioPacket), state.SessionID, time.Now().UnixMilli())

		// 模拟实时发送间隔
		time.Sleep(60 * time.Millisecond) // 60ms帧间隔
	}

	logger.Info("UDP音频传输完成")
	return nil
}

// MQTT文本消息测试
func testMQTTText(state *TestState) error {
	logger.Info("6. 发送MQTT文本消息...")

	textMsg := map[string]interface{}{
		"session_id": state.SessionID,
		"type":       "text",
		"text":       "你好，这是一个测试消息",
		"time":       time.Now().UnixMilli(),
	}

	textData, _ := json.Marshal(textMsg)
	if token := state.MQTTClient.Publish(state.CommonPushTopic, 1, false, textData); token.Wait() && token.Error() != nil {
		return fmt.Errorf("发布文本消息失败: %v", token.Error())
	}

	// 等待响应
	time.Sleep(WaitForHelloTime * time.Second)

	logger.Info("MQTT文本消息发送成功")
	return nil
}

// MQTT goodbye测试
func testMQTTGoodbye(state *TestState) error {
	logger.Info("7. 发送MQTT goodbye消息...")

	goodbyeMsg := &MqttMessagePayload{
		Type:      "goodbye",
		SessionID: state.SessionID,
	}

	goodbyeData, _ := json.Marshal(goodbyeMsg)
	if token := state.MQTTClient.Publish(state.CommonPushTopic, 1, false, goodbyeData); token.Wait() && token.Error() != nil {
		return fmt.Errorf("发布goodbye消息失败: %v", token.Error())
	}

	// 等待goodbye响应
	time.Sleep(WaitForGoodbyeTime * time.Second)

	logger.Info("MQTT goodbye消息发送成功,sessionID:", state.SessionID)
	return nil
}

// 处理服务端消息
func handleServerMessage(state *TestState, msg mqtt.Message) {
	var response MqttMessagePayload
	if err := json.Unmarshal(msg.Payload(), &response); err != nil {
		logger.Errorf("解析服务端消息失败: %v", err)
		return
	}

	logger.Debugf("收到服务端消息: %s", response)

	switch response.Type {
	case "hello":
		state.SessionID = response.SessionID
		state.UDP.Server = response.UDP.Server
		state.UDP.Port = response.UDP.Port
		logger.Infof("收到hello响应，SessionID: %s", state.SessionID)
		logger.Infof("收到tts_hello消息: %s", response)
	case "tts":
		switch response.State {

		case "start":
			logger.Infof("收到tts_start消息: %s", response)
		case "stop":
			logger.Infof("收到tts_stop消息: %s", response)
		case "sentence_start":
			logger.Infof("收到tts_sentence_start消息: %s", response)
		}
	case "llm":
		logger.Infof("llm_emotion: %s", response.Emotion)
	case "iot":
		logger.Infof("收到iot消息: %s", response.Commands)
	case "end_chat":
		logger.Debug("收到end_chat响应")

	case "goodbye":
		logger.Info("收到goodbye响应")
		go func() {
			logger.Debugf("设备 %s 开始转换音频数据为WAV文件", state.SessionID)

			state.AudioFramesMutex.Lock()
			if len(state.AudioFrames) == 0 {
				state.AudioFramesMutex.Unlock()
				logger.Debugf("设备 %s 没有音频数据需要转换", state.SessionID)
				return
			}

			// 创建深拷贝避免并发问题
			audioData := make([][]byte, len(state.AudioFrames))
			for i, frame := range state.AudioFrames {
				audioData[i] = make([]byte, len(frame))
				copy(audioData[i], frame)
			}
			logger.Debugf("设备 %s 音频包长度: %d", state.SessionID, len(audioData))

			// 清空原始数据
			state.AudioFrames = make([][]byte, 0)
			state.AudioFramesMutex.Unlock()

			strPath, err := OpusToWav(nil, audioData, state.SessionID)
			if err != nil {
				logger.Errorf("设备 %s 转换音频数据为WAV文件失败: %v", state.SessionID, err)
			} else {
				logger.Debugf("设备 %s 转换音频数据为WAV文件成功,路径: %s", state.SessionID, strPath)
			}
		}()
	}
}

// 生成sessionID
func generateSessionID() string {
	return fmt.Sprintf("%d", time.Now().Unix())
}

// 生成nonce
func generateNonce(state *TestState) {
	// nonce格式: 01 + timestamp(8字节) + sessionID(8字节) + 0000000
	timestamp := uint64(time.Now().Unix())

	// 填充nonce
	copy(state.Nonce[0:2], []byte{0x01, 0x00})                           // 标识符
	binary.BigEndian.PutUint64(state.Nonce[2:10], timestamp)             // 时间戳
	copy(state.Nonce[10:16], []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}) // 填充
}

// 构建音频包
func (c *TestState) buildAudioPacket(audioData []byte) ([]byte, error) {
	// 包格式: [16字节nonce][音频数据]
	// nonce格式: 0010 + size(2字节) + sessionID前5字节 + timestamp(4字节) + 自增序列号(3字节)

	// 计算nonce
	nonce := c.buildNonce(audioData)

	// 构建完整包
	packet := make([]byte, 16+len(audioData))

	// 复制nonce
	copy(packet[0:16], nonce[:])

	// 复制音频数据
	copy(packet[16:], audioData)

	// 递增序列号
	c.Sequence++

	return packet, nil
}

func (c *TestState) buildNonce(audioData []byte) [16]byte {
	var nonce [16]byte
	pos := 0

	// 1. 固定开头 0010 (2字节)
	nonce[pos] = 0x00
	nonce[pos+1] = 0x10
	pos += 2

	// 2. size (2字节) - 音频数据大小
	size := uint16(len(audioData))
	binary.BigEndian.PutUint16(nonce[pos:pos+2], size)
	pos += 2

	// 3. sessionID前5字节 - 使用固定的sessionID前缀
	sessionIDPrefixHex := c.SessionID
	sessionIDPrefixBytes, err := hex.DecodeString(sessionIDPrefixHex)
	if err != nil || len(sessionIDPrefixBytes) != 5 {
		// 如果解析失败，使用默认值
		copy(nonce[pos:pos+5], []byte{0xad, 0x09, 0xf8, 0x25, 0x00})
	} else {
		copy(nonce[pos:pos+5], sessionIDPrefixBytes)
	}
	pos += 5

	// 4. timestamp (4字节) - 毫秒级时间戳
	timestamp := uint32(time.Now().UnixNano() / 1e6)
	binary.BigEndian.PutUint32(nonce[pos:pos+4], timestamp)
	pos += 4

	// 5. 自增序列号 (3字节)
	// 将32位序列号转换为24位
	seq24 := c.Sequence & 0xFFFFFF  // 只取低24位
	nonce[pos] = byte(seq24 >> 16)  // 高8位
	nonce[pos+1] = byte(seq24 >> 8) // 中8位
	nonce[pos+2] = byte(seq24)      // 低8位

	// logger.Debugf("构建的nonce: %x", nonce)
	return nonce
}

// 辅助函数：PutUint24 将一个uint24值放入字节数组中
func PutUint24(b []byte, v uint32) {
	b[0] = byte(v >> 16)
	b[1] = byte(v >> 8)
	b[2] = byte(v)
}

// 辅助函数：Uint24 从字节数组中读取一个uint24值
func Uint24(b []byte) uint32 {
	return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
}

// 哈希MAC地址
func hashMAC(mac string) string {
	hash := md5.Sum([]byte(mac))
	return hex.EncodeToString(hash[:])
}

// 转换WAV到Opus (简化版本)

// 清理资源
func cleanup(state *TestState) {
	logger.Info("8. 清理资源...")

	if state.MQTTClient != nil {
		state.MQTTClient.Disconnect(250)
	}

	if state.UDPConn != nil {
		state.UDPConn.Close()
	}

	logger.Info("资源清理完成")
}

// 运行测试
func TestMain(m *testing.M) {
	// 设置日志级别
	logger.Debug("开始集成测试")

	// 运行测试
	code := m.Run()

	// 退出
	os.Exit(code)
}

func deToHex(decimal int) string {
	return fmt.Sprintf("%x", decimal)
}

type AudioConfig struct {
	SampleRate    int
	Channels      int
	FrameDuration int // 毫秒
}

func ConvertWavToOpus(wavPath string, config AudioConfig) ([][]byte, error) {
	// 验证配置参数
	if config.SampleRate <= 0 || config.Channels <= 0 || config.FrameDuration <= 0 {
		return nil, fmt.Errorf("无效的音频配置: SampleRate=%d, Channels=%d, FrameDuration=%d",
			config.SampleRate, config.Channels, config.FrameDuration)
	}

	frameSize, err := getValidFrameSize(config.SampleRate, config.FrameDuration)
	if err != nil {
		return nil, err
	}

	// 打开WAV文件
	f, err := os.Open(wavPath)
	if err != nil {
		return nil, fmt.Errorf("打开WAV文件失败: %w", err)
	}
	defer f.Close()

	// 创建WAV解码器
	decoder := wav.NewDecoder(f)
	if !decoder.IsValidFile() {
		return nil, errors.New("无效的WAV文件格式")
	}

	// 读取PCM数据
	buf, err := decoder.FullPCMBuffer()
	if err != nil {
		return nil, fmt.Errorf("读取PCM数据失败: %w", err)
	}

	// 获取音频信息
	origSampleRate := decoder.SampleRate
	origChannels := decoder.NumChans
	origBitDepth := decoder.BitDepth

	logger.Debug("音频信息",
		"原始采样率", origSampleRate,
		"原始声道数", origChannels,
		"原始位深", origBitDepth,
		"目标采样率", config.SampleRate,
		"目标声道数", config.Channels,
		"音频总长度(samples)", len(buf.Data),
		"预计音频时长(s)", float64(len(buf.Data))/float64(origSampleRate))

	logger.Debug("编码参数",
		"帧大小(samples)", frameSize,
		"帧时长(ms)", config.FrameDuration)

	logger.Debug("frameSize", frameSize)

	// 初始化opus帧切片
	opusFrames := make([][]byte, 0)

	encoder, err := gopus.NewEncoder(config.SampleRate, config.Channels, gopus.Voip)
	if err != nil {
		return nil, fmt.Errorf("创建opus编码器失败: %w", err)
	}

	encoder.SetBitrate(192000)

	bufFloat := buf.AsFloat32Buffer().Data
	bufFloat2 := make([]float32, len(bufFloat))

	if int(origSampleRate) != config.SampleRate {
		r, w := resampler.Resample32(bufFloat, int(origSampleRate), bufFloat2, config.SampleRate, 10)
		logger.Debug("r", r, "w", w)
		bufFloat = bufFloat2[:w]
	}

	// logger.Debug("bufFloat", len(bufFloat))

	bufInt16 := make([]int16, len(bufFloat))
	// logger.Debug("bufFloat", bufFloat)
	for i, value := range bufFloat {
		bufInt16[i] = cast.ToInt16(value * 32767)
	}
	// logger.Debug("bufInt16", bufInt16)
	numFrames := len(bufInt16) / frameSize
	// logger.Debug("len(buf.Data)", len(bufInt16))
	// logger.Debug("numFrames", numFrames)

	// logger.Debug("frameSize", frameSize)

	for i := 0; i < numFrames; i++ {
		frame := bufInt16[i*frameSize : (i+1)*frameSize]
		// logger.Debug("frame len", len(frame))
		// Opus要求的最大数据大小
		data := make([]byte, 2000) // 最大帧大小
		// pcmFrame := int16ToBytes(bufInt16)

		// logger.Debug("pcmFrame len", len(pcmFrame))
		data, err := encoder.Encode(frame, frameSize, len(data))
		if err != nil {
			return nil, fmt.Errorf("编码opus帧失败: %w", err)
		}
		opusFrames = append(opusFrames, data)
		// logger.Debug("编码帧大小", len(data), "bytes")
		// os.WriteFile(fmt.Sprintf("./opus_frames/opus_frame_%d.opus", i), data, 0644)

	}

	return opusFrames, nil
}

// // 验证并获取有效的帧大小
func getValidFrameSize(sampleRate, frameDuration int) (int, error) {
	// Opus支持的帧大小（毫秒）
	validDurations := []int{2, 5, 10, 20, 40, 60}
	found := false
	for _, d := range validDurations {
		if frameDuration == d {
			found = true
			break
		}
	}
	if !found {
		return 0, fmt.Errorf("无效的帧时长 %d ms，必须是以下值之一：%v", frameDuration, validDurations)
	}

	return frameDuration * sampleRate / 1000, nil
}
func HashMAC(mac string) string {
	hash := md5.Sum([]byte(mac))
	return hex.EncodeToString(hash[:])
}

func GetServerTopic(mac string) string {
	// 将 mac 从 10:89：db变成 10_89_db
	return "devices/p2p/" + strings.ReplaceAll(mac, ":", "_")
}

// 生成设备配置文件
func generateDeviceConfig() error {
	devices := []DeviceInfo{}

	// 生成多个设备配置
	for i := 0; i < ConcurrentTestCount; i++ {
		logger.Debugf("ConcurrentTestCount: %d", i)
		device := DeviceInfo{
			Version:             "2",
			Language:            "zh-CN",
			FlashSize:           16777216,
			MinimumFreeHeapSize: 8441000,
			MacAddress:          fmt.Sprintf("bb:51:db:72:70:%02x", i),
			UUID:                fmt.Sprintf("3c05ed7d-eae5-41ec-aebf-c284c9ddce%02x", 0x90+i),
			ChipModelName:       "esp32s3",
			Application: AppInfo{
				Name:        "xiaozhi",
				Version:     "2.1.0",
				CompileTime: "Jul 15 2025T15:28:46Z",
				IDFVersion:  "v5.3.1-dirty",
			},
			Board: BoardInfo{
				Type:     "tudouzi",
				Name:     "tudouzi",
				Revision: "ML307R-DL-MBRH0S01",
				Carrier:  "CHINA MOBILE",
				CSQ:      "26",
				IMEI:     fmt.Sprintf("4602404297598%02d", 40+i),
				ICCID:    fmt.Sprintf("898608421024819348%02d", 40+i),
			},
		}
		devices = append(devices, device)
	}

	config := DeviceConfig{Devices: devices}

	// 写入JSON文件
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化设备配置失败: %v", err)
	}

	err = os.WriteFile(DeviceConfigFile, data, 0644)
	if err != nil {
		return fmt.Errorf("写入设备配置文件失败: %v", err)
	}

	logger.Infof("设备配置文件已生成: %s", DeviceConfigFile)
	return nil
}

// 读取设备配置
func loadDeviceConfig() ([]DeviceInfo, error) {
	data, err := os.ReadFile(DeviceConfigFile)
	if err != nil {
		return nil, fmt.Errorf("读取设备配置文件失败: %v", err)
	}

	var config DeviceConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析设备配置文件失败: %v", err)
	}

	return config.Devices, nil
}

// 随机选择WAV文件
func getRandomWavFile() string {
	// 可用的WAV文件列表
	wavFiles := []string{
		"./天问一号.wav",
		"./你好.wav",
		"./可以说话.wav",
		"./你能教我说日语吗.wav",
		"./你是谁.wav",
		// "./statics/audio/asr_06551d0d44_.wav",
		// "./statics/audio/asr_09b2c5973c_.wav",
		// "./statics/audio/asr_1857e90cc3_.wav",
		// "./statics/audio/asr_1aabfe0625_.wav",
		// "./statics/audio/asr_1e55e39e43_.wav",
		// "./statics/audio/asr_2050b9a04f_.wav",
		// "./statics/audio/asr_2bbf9c75dd_.wav",
		// "./statics/audio/asr_37fb96a647_.wav",
		// "./statics/audio/asr_43d9c8bbe6_.wav",
		// "./statics/audio/asr_470d4f1fab_.wav",
		// "./statics/audio/asr_4b7ddf5564_.wav",
		// "./statics/audio/asr_4d36deb6d9_.wav",
		// "./statics/audio/asr_50ca81e029_.wav",
		// "./statics/audio/asr_55bb6daefd_.wav",
		// "./statics/audio/asr_5961bb4a71_.wav",
		// "./statics/audio/asr_6b4c8a165c_.wav",
		// "./statics/audio/asr_6ba58036c4_.wav",
		// "./statics/audio/asr_6e325af99c_.wav",
		// "./statics/audio/asr_72c53299b4_.wav",
		// "./statics/audio/asr_8e7b25a1c1_.wav",
		// "./statics/audio/asr_91e1200522_.wav",
		// "./statics/audio/asr_976efdc744_.wav",
		// "./statics/audio/asr_97a4e7d8e9_.wav",
		// "./statics/audio/asr_b03b656bb7_.wav",
		// "./statics/audio/asr_c4ec94f136_.wav",
		// "./statics/audio/asr_ca13a7471a_.wav",
		// "./statics/audio/asr_d103655760_.wav",
		// "./statics/audio/asr_dc2d0534e3_.wav",
		// "./statics/audio/asr_dce41cdb03_.wav",
		// "./statics/audio/asr_dfc325e9bb_.wav",
		// "./statics/audio/asr_e02e29c179_.wav",
		// "./statics/audio/asr_eb7816c629_.wav",
		// "./statics/audio/asr_ef72e42ad3_.wav",
		// "./statics/audio/asr_f8de0d7214_.wav",
	}

	// 随机选择一个文件
	selectedFile := wavFiles[rand.Intn(len(wavFiles))]

	// 检查文件是否存在，如果不存在则使用默认文件
	if _, err := os.Stat(selectedFile); os.IsNotExist(err) {
		logger.Warnf("随机选择的WAV文件不存在: %s，使用默认文件", selectedFile)
		return "./你好.wav"
	}

	return selectedFile
}

// 单个设备测试函数
func runSingleDeviceTest(device DeviceInfo, wg *sync.WaitGroup) {
	defer wg.Done()

	result := TestResult{
		DeviceMAC: device.MacAddress,
		StartTime: time.Now(),
		Success:   false,
	}

	logger.Infof("开始测试设备: %s", device.MacAddress)

	// 初始化测试状态
	state := &TestState{
		Sequence:           1,
		ReceivedText:       make([]string, 0),
		ReceivedAudio:      make([]int, 0),
		CommonPushTopic:    "device-server",
		SubServerTopic:     GetServerTopic(device.MacAddress),
		AcceptAudioPacket:  &AudioPacket{},
		MQTT:               MQTT{},
		UDP:                UDP{},
		AudioFrames:        make([][]byte, 0),
		FirstAudioReceived: false,
	}

	// 1. HTTP认证
	authStart := time.Now()
	authResp, err := testHTTPAuthWithDevice(state, device)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("HTTP认证失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.AuthTime = result.EndTime.Sub(authStart)
		addTestResult(result)
		logger.Errorf("设备 %s HTTP认证失败: %v", device.MacAddress, err)
		return
	}
	result.AuthTime = time.Since(authStart)

	// 2. MQTT连接和hello握手
	helloStart := time.Now()
	if sessionID, err := testMQTTHelloWithDevice(authResp, state, device); err != nil {
		result.ErrorMessage = fmt.Sprintf("MQTT hello握手失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.HelloTime = result.EndTime.Sub(helloStart)
		addTestResult(result)
		logger.Errorf("设备 %s MQTT hello握手失败: %v", device.MacAddress, err)
		return
	} else {
		state.SessionID = sessionID
		result.SessionID = sessionID
	}
	result.HelloTime = time.Since(helloStart)

	// 3. 发送IoT消息
	iotStart := time.Now()
	if err := testMQTTIoTWithDevice(state, device); err != nil {
		result.ErrorMessage = fmt.Sprintf("MQTT IoT消息失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.IoTTime = result.EndTime.Sub(iotStart)
		addTestResult(result)
		logger.Errorf("设备 %s MQTT IoT消息失败: %v", device.MacAddress, err)
		return
	}
	result.IoTTime = time.Since(iotStart)

	// 4. 发送listen start消息
	listenStart := time.Now()
	if err := testMQQTTListenStartWithDevice(state, device); err != nil {
		result.ErrorMessage = fmt.Sprintf("MQTT listen start消息失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.ListenTime = result.EndTime.Sub(listenStart)
		addTestResult(result)
		logger.Errorf("设备 %s MQTT listen start消息失败: %v", device.MacAddress, err)
		return
	}

	// 5. 建立UDP连接
	if err := setupUDPConnection(state); err != nil {
		result.ErrorMessage = fmt.Sprintf("UDP连接建立失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.ListenTime = result.EndTime.Sub(listenStart)
		addTestResult(result)
		logger.Errorf("设备 %s UDP连接建立失败: %v", device.MacAddress, err)
		return
	}

	// 等待UDP客户端启动
	time.Sleep(500 * time.Millisecond)

	// 6. UDP音频传输
	audioStart := time.Now()
	wavFile := getRandomWavFile()
	if err := testUDPAudioWithFile(state, wavFile); err != nil {
		result.ErrorMessage = fmt.Sprintf("UDP音频传输失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.AudioTime = result.EndTime.Sub(audioStart)
		addTestResult(result)
		logger.Errorf("设备 %s UDP音频传输失败: %v", device.MacAddress, err)
		return
	}
	result.AudioTime = time.Since(audioStart)

	// 7. 发送listen stop消息
	if err := testMQQTTListenStopWithDevice(state, device); err != nil {
		result.ErrorMessage = fmt.Sprintf("MQTT listen stop消息失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.ListenTime = result.EndTime.Sub(listenStart)
		addTestResult(result)
		logger.Errorf("设备 %s MQTT listen stop消息失败: %v", device.MacAddress, err)
		return
	}
	result.ListenTime = time.Since(listenStart)

	// 启动UDP客户端监听
	go testUDPClient(state)

	// 8. 发送文本消息
	textStart := time.Now()
	if err := testMQTTTextWithDevice(state, device); err != nil {
		result.ErrorMessage = fmt.Sprintf("MQTT文本消息失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.TextTime = result.EndTime.Sub(textStart)
		addTestResult(result)
		logger.Errorf("设备 %s MQTT文本消息失败: %v", device.MacAddress, err)
		return
	}
	result.TextTime = time.Since(textStart)

	// 9. 发送goodbye消息
	goodbyeStart := time.Now()
	time.Sleep(15 * time.Second)
	if err := testMQTTGoodbyeWithDevice(state, device); err != nil {
		logger.Errorf("设备 %s MQTT goodbye失败: %v", device.MacAddress, err)
	}

	// 等待测试完成
	time.Sleep(WaitForEndTime * time.Second)
	result.GoodbyeTime = time.Since(goodbyeStart)

	// 统计音频包数量
	state.AudioFramesMutex.Lock()
	result.AudioPackets = len(state.AudioFrames)
	state.AudioFramesMutex.Unlock()

	// 统计文本消息数量
	result.TextMessages = len(state.ReceivedText)

	// 清理资源
	cleanup(state)

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.Success = true

	addTestResult(result)
	logger.Infof("设备 %s 测试完成，耗时: %v", device.MacAddress, result.Duration)
}

// 添加测试结果
func addTestResult(result TestResult) {
	testResultsLock.Lock()
	defer testResultsLock.Unlock()
	testResults = append(testResults, result)
}

// 打印测试结果统计
func printTestResults() {
	testResultsLock.Lock()
	defer testResultsLock.Unlock()

	logger.Info("=== 并发测试结果统计 ===")

	totalTests := len(testResults)
	successTests := 0
	var totalDuration time.Duration
	var totalAuthTime time.Duration
	var totalHelloTime time.Duration
	var totalIoTTime time.Duration
	var totalListenTime time.Duration
	var totalAudioTime time.Duration
	var totalTextTime time.Duration
	var totalGoodbyeTime time.Duration
	var totalAudioPackets int
	var totalTextMessages int

	for _, result := range testResults {
		if result.Success {
			successTests++
			totalDuration += result.Duration
			totalAuthTime += result.AuthTime
			totalHelloTime += result.HelloTime
			totalIoTTime += result.IoTTime
			totalListenTime += result.ListenTime
			totalAudioTime += result.AudioTime
			totalTextTime += result.TextTime
			totalGoodbyeTime += result.GoodbyeTime
			totalAudioPackets += result.AudioPackets
			totalTextMessages += result.TextMessages
		}
	}

	logger.Infof("总测试数: %d", totalTests)
	logger.Infof("成功测试数: %d", successTests)
	logger.Infof("成功率: %.2f%%", float64(successTests)/float64(totalTests)*100)

	if successTests > 0 {
		logger.Infof("平均总耗时: %v", totalDuration/time.Duration(successTests))
		logger.Infof("平均认证耗时: %v", totalAuthTime/time.Duration(successTests))
		logger.Infof("平均Hello耗时: %v", totalHelloTime/time.Duration(successTests))
		logger.Infof("平均IoT耗时: %v", totalIoTTime/time.Duration(successTests))
		logger.Infof("平均Listen耗时: %v", totalListenTime/time.Duration(successTests))
		logger.Infof("平均音频传输耗时: %v", totalAudioTime/time.Duration(successTests))
		logger.Infof("平均文本消息耗时: %v", totalTextTime/time.Duration(successTests))
		logger.Infof("平均Goodbye耗时: %v", totalGoodbyeTime/time.Duration(successTests))
		logger.Infof("总音频包数: %d", totalAudioPackets)
		logger.Infof("总文本消息数: %d", totalTextMessages)
	}

	// 打印失败的测试
	for _, result := range testResults {
		if !result.Success {
			logger.Errorf("设备 %s 测试失败: %s", result.DeviceMAC, result.ErrorMessage)
		}
	}

	// 保存详细结果到JSON文件
	saveDetailedResults()
}

// 保存详细结果到JSON文件
func saveDetailedResults() {
	data, err := json.MarshalIndent(testResults, "", "  ")
	if err != nil {
		logger.Errorf("序列化测试结果失败: %v", err)
		return
	}

	filename := fmt.Sprintf("statics/test_json/test_results_%s.json", time.Now().Format("20060102_150405"))
	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		logger.Errorf("保存测试结果失败: %v", err)
		return
	}

	logger.Infof("详细测试结果已保存到: %s", filename)
}

// 带设备的HTTP认证测试
func testHTTPAuthWithDevice(state *TestState, device DeviceInfo) (AuthResponse, error) {
	logger.Infof("设备 %s 开始HTTP认证...", device.MacAddress)

	// 发送HTTP请求
	jsonData, err := json.Marshal(device)
	if err != nil {
		return AuthResponse{}, fmt.Errorf("序列化认证请求失败: %v", err)
	}

	resp, err := http.Post(
		fmt.Sprintf("http://%s:%d/devices/auth", ServerHost, ServerPort),
		"application/json",
		strings.NewReader(string(jsonData)),
	)
	if err != nil {
		return AuthResponse{}, fmt.Errorf("HTTP请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return AuthResponse{}, fmt.Errorf("HTTP认证失败，状态码: %d", resp.StatusCode)
	}

	// 解析响应
	var authResp AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return AuthResponse{}, fmt.Errorf("解析认证响应失败: %v", err)
	}

	logger.Infof("设备 %s HTTP认证成功", device.MacAddress)

	state.CommonPushTopic = authResp.MQTT.PublishTopic
	state.MQTT.EndPoint = authResp.MQTT.Endpoint
	state.MQTT.Username = authResp.MQTT.Username
	state.MQTT.Password = authResp.MQTT.Password
	state.MQTT.ClientID = authResp.MQTT.ClientID
	state.MQTT.CommonPushTopic = authResp.MQTT.PublishTopic

	return authResp, nil
}

// 带设备的MQTT hello握手测试
func testMQTTHelloWithDevice(authResp AuthResponse, state *TestState, device DeviceInfo) (string, error) {
	logger.Infof("设备 %s 开始MQTT hello握手...", device.MacAddress)

	// 生成sessionID
	state.SessionID = generateSessionID()

	// 生成nonce
	generateNonce(state)

	// 构建hello消息
	helloMsg := map[string]interface{}{
		"type":      "hello",
		"version":   3,
		"transport": "udp",
		"audio_params": map[string]interface{}{
			"format":         "opus",
			"sample_rate":    16000,
			"channels":       1,
			"frame_duration": 60,
		},
		"device_info": map[string]interface{}{
			"mac": device.MacAddress,
		},
	}

	// 连接MQTT
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s", state.MQTT.EndPoint))

	// 为每个设备生成唯一的客户端ID，避免连接冲突
	uniqueClientID := fmt.Sprintf("%s_%s_%d", state.MQTT.ClientID, device.MacAddress, time.Now().UnixNano())
	opts.SetClientID(uniqueClientID)
	logger.Infof("设备 %s 使用唯一客户端ID: %s", device.MacAddress, uniqueClientID)
	logger.Debugf("state.MQTT.ClientID: %s", state.MQTT.ClientID)

	opts.SetUsername(state.MQTT.Username)
	opts.SetPassword(state.MQTT.Password)
	opts.SetResumeSubs(true)
	opts.SetCleanSession(false)
	opts.SetKeepAlive(30 * time.Second)
	opts.SetPingTimeout(10 * time.Second)

	state.MQTTClient = mqtt.NewClient(opts)
	if token := state.MQTTClient.Connect(); token.Wait() && token.Error() != nil {
		return "", fmt.Errorf("MQTT连接失败: %v", token.Error())
	}

	// 订阅公共主题
	logger.Infof("设备 %s 订阅自己的主题: %s", device.MacAddress, GetServerTopic(device.MacAddress))
	if token := state.MQTTClient.Subscribe(GetServerTopic(device.MacAddress), 1, func(client mqtt.Client, msg mqtt.Message) {
		handleServerMessage(state, msg)
	}); token.Wait() && token.Error() != nil {
		return "", fmt.Errorf("MQTT订阅失败: %v", token.Error())
	}

	// 发送hello消息
	logger.Infof("设备 %s 发送hello消息到公共主题：%s", device.MacAddress, state.CommonPushTopic)
	helloData, _ := json.Marshal(helloMsg)
	if token := state.MQTTClient.Publish(state.CommonPushTopic, 1, false, helloData); token.Wait() && token.Error() != nil {
		return "", fmt.Errorf("MQTT发布hello失败: %v", token.Error())
	}

	// 等待hello响应
	time.Sleep(WaitForHelloTime * time.Second)

	if state.SessionID == "" {
		return "", fmt.Errorf("未收到hello响应")
	}

	logger.Infof("设备 %s MQTT hello握手成功，SessionID: %s", device.MacAddress, state.SessionID)
	return state.SessionID, nil
}

// 带设备的MQTT IoT消息测试
func testMQTTIoTWithDevice(state *TestState, device DeviceInfo) error {
	logger.Infof("设备 %s 发送MQTT IoT消息...", device.MacAddress)

	// 检查MQTT连接状态
	if !state.MQTTClient.IsConnected() {
		return fmt.Errorf("MQTT客户端未连接")
	}

	logger.Infof("设备 %s 订阅服务端主题: %s", device.MacAddress, state.SubServerTopic)

	// 订阅服务端主题
	if token := state.MQTTClient.Subscribe(state.SubServerTopic, 1, func(client mqtt.Client, msg mqtt.Message) {
		handleServerMessage(state, msg)
	}); token.Wait() && token.Error() != nil {
		return fmt.Errorf("订阅服务端主题失败: %v", token.Error())
	}

	// 发送IoT消息
	iotMsg := map[string]interface{}{
		"session_id": state.SessionID,
		"type":       "iot",
		"update":     true,
		"descriptors": []map[string]interface{}{
			{
				"name":        "Speaker",
				"description": "扬声器",
				"properties": map[string]interface{}{
					"volume": map[string]interface{}{
						"description": "当前音量值",
						"type":        "number",
					},
				},
				"methods": map[string]interface{}{
					"SetVolume": map[string]interface{}{
						"description": "设置音量",
						"parameters": map[string]interface{}{
							"volume": map[string]interface{}{
								"description": "0到100之间的整数",
								"type":        "number",
							},
						},
					},
				},
			},
		},
	}

	iotData, _ := json.Marshal(iotMsg)
	if token := state.MQTTClient.Publish(state.CommonPushTopic, 1, false, iotData); token.Wait() && token.Error() != nil {
		return fmt.Errorf("发布IoT消息失败: %v", token.Error())
	}

	// 发送设备状态
	stateMsg := map[string]interface{}{
		"session_id": state.SessionID,
		"type":       "iot",
		"update":     true,
		"states": []map[string]interface{}{
			{
				"name": "Speaker",
				"state": map[string]interface{}{
					"volume": 90,
				},
			},
			{
				"name": "Battery",
				"state": map[string]interface{}{
					"level":    80,
					"charging": false,
				},
			},
		},
	}

	stateData, _ := json.Marshal(stateMsg)
	if token := state.MQTTClient.Publish(state.CommonPushTopic, 1, false, stateData); token.Wait() && token.Error() != nil {
		return fmt.Errorf("发布设备状态失败: %v", token.Error())
	}

	logger.Infof("设备 %s MQTT IoT消息发送成功", device.MacAddress)
	return nil
}

// 带设备的MQTT listen start消息测试
func testMQQTTListenStartWithDevice(state *TestState, device DeviceInfo) error {
	logger.Infof("设备 %s 发送MQTT listen start消息...", device.MacAddress)

	listenMsg := map[string]interface{}{
		"session_id": state.SessionID,
		"type":       "listen",
		"mode":       "manual",
		"state":      "start",
	}

	listenData, _ := json.Marshal(listenMsg)
	if token := state.MQTTClient.Publish(state.CommonPushTopic, 1, false, listenData); token.Wait() && token.Error() != nil {
		return fmt.Errorf("发布listen start消息失败: %v", token.Error())
	}

	logger.Infof("设备 %s MQTT listen start消息发送成功", device.MacAddress)
	return nil
}

// 带设备的MQTT listen stop消息测试
func testMQQTTListenStopWithDevice(state *TestState, device DeviceInfo) error {
	logger.Infof("设备 %s 发送MQTT listen stop消息...", device.MacAddress)

	listenMsg := &MqttMessagePayload{
		SessionID: state.SessionID,
		Type:      "listen",
		State:     "stop",
	}

	listenData, _ := json.Marshal(listenMsg)
	if token := state.MQTTClient.Publish(state.CommonPushTopic, 1, false, listenData); token.Wait() && token.Error() != nil {
		return fmt.Errorf("发布listen stop消息失败: %v", token.Error())
	}

	// 记录listen stop发送时间
	state.ListenStopTime = time.Now()
	logger.Infof("设备 %s MQTT listen stop消息发送成功,topic: %s, 时间: %v", state.SessionID, state.CommonPushTopic, state.ListenStopTime)
	return nil
}

// 带文件的UDP音频传输测试
func testUDPAudioWithFile(state *TestState, wavFile string) error {
	logger.Infof("开始UDP音频传输，使用文件: %s", wavFile)

	// 检查测试音频文件
	if _, err := os.Stat(wavFile); os.IsNotExist(err) {
		logger.Warnf("测试音频文件不存在: %s，跳过音频传输测试", wavFile)
		return nil
	}

	// 转换WAV到Opus
	opusFrames, err := ConvertWavToOpus(wavFile, AudioConfig{
		SampleRate:    16000,
		Channels:      1,
		FrameDuration: 60,
	})
	if err != nil {
		return fmt.Errorf("转换WAV到Opus失败: %v", err)
	}

	logger.Infof("开始发送 %d 个音频帧", len(opusFrames))

	// 发送音频帧
	for _, frame := range opusFrames {
		// 构建音频包
		audioPacket, err := state.buildAudioPacket(frame)
		if err != nil {
			return fmt.Errorf("构建音频包失败: %v", err)
		}

		// 发送音频包
		_, err = state.UDPConn.Write(audioPacket)
		if err != nil {
			return fmt.Errorf("发送音频包失败: %v", err)
		}

		// logger.Debugf("发送音频包 %d/%d，大小: %d bytes,sessionID: %s, 时间戳: %d", i+1, len(opusFrames), len(audioPacket), state.SessionID, time.Now().UnixMilli())

		// 模拟实时发送间隔
		time.Sleep(60 * time.Millisecond) // 60ms帧间隔
	}

	logger.Info("UDP音频传输完成")
	return nil
}

// 带设备的MQTT文本消息测试
func testMQTTTextWithDevice(state *TestState, device DeviceInfo) error {
	logger.Infof("设备 %s 发送MQTT文本消息...", device.MacAddress)

	textMsg := map[string]interface{}{
		"session_id": state.SessionID,
		"type":       "text",
		"text":       "你好，这是一个测试消息",
		"time":       time.Now().UnixMilli(),
	}

	textData, _ := json.Marshal(textMsg)
	if token := state.MQTTClient.Publish(state.CommonPushTopic, 1, false, textData); token.Wait() && token.Error() != nil {
		return fmt.Errorf("发布文本消息失败: %v", token.Error())
	}

	// 等待响应
	time.Sleep(WaitForHelloTime * time.Second)

	logger.Infof("设备 %s MQTT文本消息发送成功", device.MacAddress)
	return nil
}

// 带设备的MQTT goodbye测试
func testMQTTGoodbyeWithDevice(state *TestState, device DeviceInfo) error {
	logger.Infof("设备 %s topic: %s 发送MQTT goodbye消息...", device.MacAddress, state.CommonPushTopic)

	goodbyeMsg := &MqttMessagePayload{
		Type:      "goodbye",
		SessionID: state.SessionID,
	}

	goodbyeData, _ := json.Marshal(goodbyeMsg)
	if token := state.MQTTClient.Publish(state.CommonPushTopic, 1, false, goodbyeData); token.Wait() && token.Error() != nil {
		return fmt.Errorf("发布goodbye消息失败: %v", token.Error())
	}

	// 等待goodbye响应
	time.Sleep(WaitForGoodbyeTime * time.Second)

	logger.Infof("设备 %s MQTT goodbye消息发送成功,sessionID: %s,topic: %s", device.MacAddress, state.SessionID, state.CommonPushTopic)
	return nil
}

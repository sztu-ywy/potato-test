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

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-audio/wav"
	"github.com/oov/audio/resampler"
	"github.com/spf13/cast"
	"github.com/uozi-tech/cosy/logger"
	"layeh.com/gopus"
)

// 测试配置
const (
	ServerHost = "localhost"
	ServerPort = 8084
	MQTTHost   = "10.14.2.54"
	MQTTPort   = 1883
	// UDPHost    = "10.151.2.9"
	// UDPPort    = 8888
	UDPHost        = "2huo.tech"
	UDPPort        = 50400
	DeviceMAC      = "10:51:db:72:70:a8"
	DeviceUUID     = "3c05ed7d-eae5-41ec-aebf-c284c9ddce90"
	TestWavFile    = "./天问一号.wav"
	TestDuration   = 5 * time.Second
	SampleInterval = 100 * time.Millisecond
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
}
type UDP struct {
	Host string
	Port int
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

// 集成测试主函数
// func TestIntegration(t *testing.T) {
func main() {
	logger.Info("开始集成测试...")

	aiOpusFrame = make([][]byte, 0)

	state := &TestState{
		Sequence:          1,
		ReceivedText:      make([]string, 0),
		ReceivedAudio:     make([]int, 0),
		CommonPushTopic:   "device-server",
		SubServerTopic:    GetServerTopic(DeviceMAC),
		AcceptAudioPacket: &AudioPacket{},
		MQTT:              MQTT{},
		UDP:               UDP{},
	}

	// 启动定时器，每3秒转换音频数据
	// go startAudioConversionTimer(state)

	// 1. HTTP认证
	authResp, err := testHTTPAuth(state)
	if err != nil {
		logger.Errorf("HTTP认证失败: %v", err)
		os.Exit(1)
	}

	// 2. MQTT连接和hello握手
	if sessionID, err := testMQTTHello(authResp, state); err != nil {
		logger.Errorf("MQTT hello握手失败: %v", err)
		os.Exit(1)
	} else {
		state.SessionID = sessionID
	}

	// 3. 发送IoT消息
	if err := testMQTTIoT(state); err != nil {
		logger.Errorf("MQTT IoT消息失败: %v", err)
		os.Exit(1)
	}

	// 3. 发送IoT消息
	if err := testMQQTTListenStart(state); err != nil {
		logger.Errorf("MQTT listen start消息失败: %v", err)
		os.Exit(1)
	}

	// 4. 发送listen消息
	// if err := testMQTTListen(state); err != nil {
	// 	logger.Errorf("MQTT listen消息失败: %v", err)
	// 	os.Exit(1)
	// }

	// 5. 建立UDP连接（不发送音频）
	if err := setupUDPConnection(state); err != nil {
		logger.Errorf("UDP连接建立失败: %v", err)
		os.Exit(1)
	}

	// 6. 启动UDP客户端监听

	// 等待UDP客户端启动
	time.Sleep(500 * time.Millisecond)

	// 7. UDP音频传输
	if err := testUDPAudio(state); err != nil {
		logger.Errorf("UDP音频传输失败: %v", err)
		os.Exit(1)
	}
	go testUDPClient(state)

	// 8. 发送listen消息
	if err := testMQQTTListenStop(state); err != nil {
		logger.Errorf("MQTT listen stop消息失败: %v", err)
		os.Exit(1)
	}

	// 7. 发送文本消息
	if err := testMQTTText(state); err != nil {
		logger.Errorf("MQTT文本消息失败: %v", err)
	}

	go func() {
		// 8. MQTT goodbye
		time.Sleep(10 * time.Second)
		if err := testMQTTGoodbye(state); err != nil {
			logger.Errorf("MQTT goodbye失败: %v", err)
		}
	}()

	time.Sleep(WaitForEndTime * time.Second)

	// 9. 清理资源
	cleanup(state)

	logger.Info("集成测试完成")
}

var aiOpusData [][]byte

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

var aiOpusFrame [][]byte
var aiOpusFrameMutex sync.Mutex


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
			count++
			logger.Debugf("成功接收音频包 count: %d, 数据大小: %d bytes, 时间戳: %d", count, n, time.Now().UnixMilli())
		}
	}
}



var saveAudioData = [][]byte{}

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

	aiOpusFrameMutex.Lock()
	// 深拷贝音频数据，避免引用问题
	audioDataCopy := make([]byte, len(packet.AudioData))
	copy(audioDataCopy, packet.AudioData)

	aiOpusFrame = append(aiOpusFrame, audioDataCopy)
	saveAudioData = append(saveAudioData, audioDataCopy)

	if len(aiOpusFrame) == 40 {
		// 验证数据一致性
		if len(aiOpusFrame[0]) == len(saveAudioData[0]) {
			isEqual := true
			for i := 0; i < len(aiOpusFrame[0]); i++ {
				if aiOpusFrame[0][i] != saveAudioData[0][i] {
					isEqual = false
					logger.Debugf("数据不一致，位置 %d: aiOpusFrame[0][%d]=%d, saveAudioData[0][%d]=%d", i, i, aiOpusFrame[0][i], i, saveAudioData[0][i])
					break
				}
			}
			if isEqual {
				logger.Debug("数据一致性验证通过")
			}
		} else {
			logger.Debugf("数据长度不一致: aiOpusFrame[0]=%d, saveAudioData[0]=%d", len(aiOpusFrame[0]), len(saveAudioData[0]))
		}
	}
	// packetCount := len(aiOpusFrame)
	aiOpusFrameMutex.Unlock()

	// logger.Infof("收到来自 %s 的音频数据包，大小: %d bytes，累计: %d 包",
	// 	clientAddr, len(packet.AudioData), packetCount)

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
		MacAddress:          DeviceMAC,
		UUID:                DeviceUUID,
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
			"mac": DeviceMAC,
		},
	}

	// 连接MQTT
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s", state.MQTT.EndPoint))
	opts.SetClientID(state.MQTT.ClientID)
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
	logger.Info("设备订阅自己的主题: ", GetServerTopic(DeviceMAC))
	if token := state.MQTTClient.Subscribe(GetServerTopic(DeviceMAC), 1, func(client mqtt.Client, msg mqtt.Message) {
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
		state.UDP.Host = response.UDP.Server
		state.UDP.Port = response.UDP.Port
		logger.Infof("收到hello响应，SessionID: %s", state.SessionID)
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

	// 连接UDP服务器
	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", state.UDP.Host, state.UDP.Port))
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
	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", state.UDP.Host, state.UDP.Port))
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

	logger.Infof("收到服务端消息: %s", response.Type)

	logger.Debug("收到服务端消息: ", response)

	switch response.Type {
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
		go func() {
			logger.Debug("开始转换音频数据为WAV文件")

			aiOpusFrameMutex.Lock()
			if len(aiOpusFrame) == 0 {
				aiOpusFrameMutex.Unlock()
				logger.Debug("没有音频数据需要转换")
				return
			}

			// 创建深拷贝避免并发问题
			audioData := make([][]byte, len(aiOpusFrame))
			for i, frame := range aiOpusFrame {
				audioData[i] = make([]byte, len(frame))
				copy(audioData[i], frame)
			}
			logger.Debug("音频包长度: ", len(audioData))

			// 清空原始数据
			aiOpusFrame = make([][]byte, 0)
			aiOpusFrameMutex.Unlock()

			strPath, err := OpusToWav(nil, audioData, state.SessionID)
			if err != nil {
				logger.Errorf("转换音频数据为WAV文件失败: %v", err)
			}
			logger.Debug("转换音频数据为WAV文件成功,路径: ", strPath)
		}()
	case "goodbye":
		logger.Info("收到goodbye响应")
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

	logger.Debugf("构建的nonce: %x", nonce)
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

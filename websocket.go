package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"time"

	"git.uozi.org/uozi/potato-api/model"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/spf13/cast"
	"github.com/uozi-tech/cosy/logger"
)

// WebSocket测试状态
type WebSocketTestState struct {
	*TestState
	WebSocketConn *websocket.Conn
	DeviceMAC     string
	SessionID     string
	Sequence      uint32
	// 每个设备独立的音频数据存储
	AudioFrames      [][]byte
	AudioFramesMutex sync.Mutex
	// 时间记录
	ListenStopTime       time.Time
	FirstAudioTime       time.Time
	FirstAudioReceived   bool
	LastAudioReceiveTime time.Time
	// WebSocket消息通道
	MessageChan chan WebSocketMessage
	// 连接状态
	Connected bool
	// 重连相关
	ReconnectAttempts    int
	MaxReconnectAttempts int
	CloseSignal          chan struct{}
}

type WebSocketMessagePayload struct {
	Type        string               `json:"type"`
	Version     int                  `json:"version,omitempty"`
	Transport   string               `json:"transport,omitempty"`
	AudioParams *AudioParams         `json:"audio_params,omitempty"`
	DeviceInfo  *WebSocketDeviceInfo `json:"device_info,omitempty"`
	SessionID   string               `json:"session_id,omitempty"`
	State       string               `json:"state,omitempty"`
	Mode        string               `json:"mode,omitempty"`
	Reason      string               `json:"reason,omitempty"`
	Text        string               `json:"text,omitempty"`
	Emotion     string               `json:"emotion,omitempty"`
	Commands    []*Command           `json:"commands,omitempty"`
	Time        int64                `json:"time,omitempty"`
	OTA         *model.OTA           `json:"ota,omitempty"`
	States      []*States            `json:"states,omitempty"`
	Update      bool                 `json:"update"`
	StatusCode  int                  `json:"status_code"`
}
type WebSocketDeviceInfo struct {
	Device        *model.Device
	MAC           string `json:"mac"`
	Identifier    string `json:"identifier"`
	SystemVersion string `json:"system_version"`
	Register      bool   `json:"register"`
}

// WebSocket消息结构
type WebSocketMessage struct {
	GinContext  *gin.Context
	SessionID   string
	Conn        *websocket.Conn
	mutex       sync.Mutex
	MessageType int    // WebSocketFrameType
	Message     []byte // WebSocketFrameRawMessage
	Data        *WebSocketMessagePayload
	Context     *WebSocketMessagePayload // 握手消息
}

// WebSocket连续对话状态
type WebSocketContinuousDialogueState struct {
	*WebSocketTestState
	RoundNumber       int
	RoundResults      []DialogueRoundResult
	CurrentRoundStart time.Time
	TotalStartTime    time.Time
}

// 全局WebSocket测试结果收集器
var (
	websocketTestResults     []TestResult
	websocketTestResultsLock sync.Mutex
)

const (
	Identifier = "tudouzi,1"
)

// WebSocket测试主函数
func runWebsocketTest() {
	logger.Info("开始WebSocket测试...")

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

	logger.Infof("准备测试 %d 个设备的WebSocket连接", len(devices))

	// 启动并发测试
	var wg sync.WaitGroup
	startTime := time.Now()

	for i, device := range devices {
		wg.Add(1)
		logger.Infof("启动设备 %d WebSocket测试: %s", i+1, device.MacAddress)
		go runSingleDeviceWebSocketTest(device, &wg)

		// 稍微延迟启动，避免同时连接过多
		time.Sleep(100 * time.Millisecond)
	}

	// 等待所有测试完成
	wg.Wait()

	totalTime := time.Since(startTime)
	logger.Infof("所有设备WebSocket测试完成，总耗时: %v", totalTime)

	// 打印测试结果统计
	printWebSocketTestResults()

	logger.Info("WebSocket测试完成")
}

// 单个设备WebSocket测试
func runSingleDeviceWebSocketTest(device DeviceInfo, wg *sync.WaitGroup) {
	defer wg.Done()

	result := TestResult{
		DeviceMAC: device.MacAddress,
		StartTime: time.Now(),
		Success:   false,
	}

	logger.Infof("开始WebSocket测试设备: %s", device.MacAddress)

	// 初始化WebSocket测试状态
	state := &WebSocketTestState{
		TestState: &TestState{
			Sequence:             1,
			ReceivedText:         make([]string, 0),
			ReceivedAudio:        make([]int, 0),
			CommonPushTopic:      "device-server",
			SubServerTopic:       GetServerTopic(device.MacAddress),
			AcceptAudioPacket:    &AudioPacket{},
			MQTT:                 MQTT{},
			UDP:                  UDP{},
			AudioFrames:          make([][]byte, 0),
			FirstAudioReceived:   false,
			ListenStopTime:       time.Now(),
			FirstAudioTime:       time.Now(),
			LastAudioReceiveTime: time.Now(),
			CloseSignal:          make(chan struct{}, 1),
		},

		DeviceMAC:            device.MacAddress,
		AudioFrames:          make([][]byte, 0),
		FirstAudioReceived:   false,
		MessageChan:          make(chan WebSocketMessage, 100),
		Connected:            false,
		ReconnectAttempts:    0,
		MaxReconnectAttempts: 3,
		CloseSignal:          make(chan struct{}, 1),
	}

	// 1. HTTP认证
	authStart := time.Now()
	authResp, err := testHTTPAuthWithDevice(state.TestState, device)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("HTTP认证失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.AuthTime = result.EndTime.Sub(authStart)
		addWebSocketTestResult(result)
		logger.Errorf("设备 %s HTTP认证失败: %v", device.MacAddress, err)
		return
	}
	result.AuthTime = time.Since(authStart)

	logger.Debugf("device info: %v", device.MacAddress)
	// 2. WebSocket连接和hello握手
	helloStart := time.Now()
	if sessionID, err := testWebSocketHello(authResp, state, device); err != nil {
		result.ErrorMessage = fmt.Sprintf("WebSocket hello握手失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.HelloTime = result.EndTime.Sub(helloStart)
		addWebSocketTestResult(result)
		logger.Errorf("设备 %s WebSocket hello握手失败: %v", device.MacAddress, err)
		return
	} else {
		state.SessionID = sessionID
		result.SessionID = sessionID
	}
	result.HelloTime = time.Since(helloStart)

	// 3. 发送IoT消息
	iotStart := time.Now()
	if err := testWebSocketIoT(state, device); err != nil {
		result.ErrorMessage = fmt.Sprintf("WebSocket IoT消息失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.IoTTime = result.EndTime.Sub(iotStart)
		addWebSocketTestResult(result)
		logger.Errorf("设备 %s WebSocket IoT消息失败: %v", device.MacAddress, err)
		return
	}
	result.IoTTime = time.Since(iotStart)

	// 4. 发送listen start消息
	listenStart := time.Now()
	if err := testWebSocketListenStart(state, device); err != nil {
		result.ErrorMessage = fmt.Sprintf("WebSocket listen start消息失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.ListenTime = result.EndTime.Sub(listenStart)
		addWebSocketTestResult(result)
		logger.Errorf("设备 %s WebSocket listen start消息失败: %v", device.MacAddress, err)
		return
	}

	// 5. 发送音频数据
	audioStart := time.Now()
	wavFile := getRandomWavFile()
	if err := testWebSocketAudio(state, wavFile); err != nil {
		result.ErrorMessage = fmt.Sprintf("WebSocket音频传输失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.AudioTime = result.EndTime.Sub(audioStart)
		addWebSocketTestResult(result)
		logger.Errorf("设备 %s WebSocket音频传输失败: %v", device.MacAddress, err)
		return
	}
	result.AudioTime = time.Since(audioStart)

	// 6. 发送listen stop消息
	if err := testWebSocketListenStop(state, device); err != nil {
		result.ErrorMessage = fmt.Sprintf("WebSocket listen stop消息失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.ListenTime = result.EndTime.Sub(listenStart)
		addWebSocketTestResult(result)
		logger.Errorf("设备 %s WebSocket listen stop消息失败: %v", device.MacAddress, err)
		return
	}
	result.ListenTime = time.Since(listenStart)

	// 6.5. 等待接收音频响应并监控音频接收情况
	logger.Infof("设备 %s 开始等待音频响应...", device.MacAddress)
	waitStartTime := time.Now()
	timeout := 30 * time.Second // 总体超时时间
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var lastAudioCount int = 0
	var audioReceiveTimeout time.Duration = 3 * time.Second // 3秒内没收到音频表示接收结束

	for {
		select {
		case <-state.CloseSignal:
			logger.Infof("设备 %s 收到关闭信号，结束等待音频响应", device.MacAddress)
			return
		case <-ticker.C:
			// 检查是否收到音频
			state.AudioFramesMutex.Lock()
			currentAudioCount := len(state.AudioFrames)
			state.AudioFramesMutex.Unlock()

			// 每10次检查打印一次调试信息
			if (time.Since(waitStartTime).Milliseconds()/100)%10 == 0 {
				logger.Debugf("设备 %s 等待音频中，当前音频包数: %d, 等待时间: %v",
					device.MacAddress, currentAudioCount, time.Since(waitStartTime))
			}

			if currentAudioCount > 0 {
				// 记录第一个音频包的时间
				if !state.FirstAudioReceived {
					state.FirstAudioReceived = true
					logger.Infof("设备 %s 收到第一个音频包，时间差: %v",
						device.MacAddress, time.Since(waitStartTime))
				}

				// 如果音频包数量增加，更新最后接收时间
				if currentAudioCount > lastAudioCount {
					state.LastAudioReceiveTime = time.Now()
					lastAudioCount = currentAudioCount
					logger.Infof("设备 %s 收到音频包，当前数量: %d",
						device.MacAddress, currentAudioCount)
				}

				// 检查是否3秒内没有收到新音频
				if !state.LastAudioReceiveTime.IsZero() &&
					time.Since(state.LastAudioReceiveTime) >= audioReceiveTimeout {

					logger.Infof("设备 %s 音频接收结束，3秒内无新音频，总音频包数: %d",
						device.MacAddress, currentAudioCount)

					// 发送save_audio消息
					if err := testWebSocketSaveAudio(state, device); err != nil {
						logger.Errorf("设备 %s 发送save_audio消息失败: %v", device.MacAddress, err)
					} else {
						logger.Infof("设备 %s 发送save_audio消息成功", device.MacAddress)
					}

					// 等待save_audio响应（音频保存完成）
					time.Sleep(2 * time.Second)
					break
				}
			}
		case <-time.After(timeout):
			logger.Errorf("设备 %s 等待音频响应超时，未收到任何音频数据", device.MacAddress)
			break
		}
	}

	// 7. 发送文本消息
	textStart := time.Now()
	if err := testWebSocketText(state, device); err != nil {
		result.ErrorMessage = fmt.Sprintf("WebSocket文本消息失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.TextTime = result.EndTime.Sub(textStart)
		addWebSocketTestResult(result)
		logger.Errorf("设备 %s WebSocket文本消息失败: %v", device.MacAddress, err)
		return
	}
	result.TextTime = time.Since(textStart)

	// 8. 发送goodbye消息
	goodbyeStart := time.Now()
	time.Sleep(15 * time.Second)
	if err := testWebSocketGoodbye(state, device); err != nil {
		logger.Errorf("设备 %s WebSocket goodbye失败: %v", device.MacAddress, err)
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
	cleanupWebSocket(state)

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.Success = true

	addWebSocketTestResult(result)
	logger.Infof("设备 %s WebSocket测试完成，耗时: %v", device.MacAddress, result.Duration)
}

// WebSocket连接和hello握手
func testWebSocketHello(authResp AuthResponse, state *WebSocketTestState, device DeviceInfo) (string, error) {
	logger.Infof("设备 %s 开始WebSocket hello握手...", device.MacAddress)

	// 生成sessionID
	state.SessionID = generateSessionID()

	// 生成nonce
	generateNonce(state.TestState)

	// 连接WebSocket
	if err := connectWebSocket(authResp, state, device); err != nil {
		return "", fmt.Errorf("WebSocket连接失败: %v", err)
	}

	helloMsg := &WebSocketMessagePayload{
		Type:      "hello",
		Version:   3,
		Transport: "websocket",
		AudioParams: &AudioParams{
			Format:        "opus",
			SampleRate:    16000,
			Channels:      1,
			FrameDuration: 60,
		},
		DeviceInfo: &WebSocketDeviceInfo{
			Device: &model.Device{
				MAC: device.MacAddress,
			},
			MAC:           device.MacAddress,
			Identifier:    Identifier,
			SystemVersion: device.Application.Version,
			Register:      true,
		},
		Time: time.Now().Unix(),
	}

	logger.Debugf("helloMsg: %v", helloMsg.DeviceInfo.Device.MAC)
	// 发送hello消息
	if err := sendWebSocketMessage(state, helloMsg); err != nil {
		return "", fmt.Errorf("发送hello消息失败: %v", err)
	}

	// 等待hello响应
	time.Sleep(WaitForHelloTime * time.Second)

	if state.SessionID == "" {
		return "", fmt.Errorf("未收到hello响应")
	}

	logger.Infof("设备 %s WebSocket hello握手成功，SessionID: %s", device.MacAddress, state.SessionID)
	return state.SessionID, nil
}

// 连接WebSocket
func connectWebSocket(authResp AuthResponse, state *WebSocketTestState, device DeviceInfo) error {
	// 构建WebSocket URL
	wsURL := authResp.WebSocket.URL
	if wsURL == "" {
		return fmt.Errorf("WebSocket URL为空")
	}

	// 添加认证token到URL
	if authResp.WebSocket.Token != "" {
		if len(wsURL) > 0 && wsURL[len(wsURL)-1] == '/' {
			wsURL = wsURL[:len(wsURL)-1]
		}
		wsURL += "?token=" + authResp.WebSocket.Token
	}

	logger.Infof("设备 %s 连接WebSocket: %s", device.MacAddress, wsURL)

	// 建立WebSocket连接
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("WebSocket连接失败: %v", err)
	}

	state.WebSocketConn = conn
	state.Connected = true

	logger.Debugf("device1 info: %v", state.DeviceMAC)
	// 启动消息接收协程
	go receiveWebSocketMessages(state)

	logger.Infof("设备 %s WebSocket连接成功", device.MacAddress)
	return nil
}

// 接收WebSocket消息
func receiveWebSocketMessages(state *WebSocketTestState) {
	defer func() {
		state.Connected = false
		if state.WebSocketConn != nil {
			state.WebSocketConn.Close()
		}
	}()

	logger.Infof("设备 %s WebSocket消息接收协程启动", state.DeviceMAC)

	for {
		if !state.Connected {
			logger.Infof("设备 %s WebSocket连接已断开，停止消息接收", state.DeviceMAC)
			break
		}

		// 设置读取超时
		state.WebSocketConn.SetReadDeadline(time.Now().Add(30 * time.Second))

		// 读取消息
		msgType, message, err := state.WebSocketConn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Errorf("设备 %s WebSocket连接异常关闭: %v", state.DeviceMAC, err)
			} else {
				logger.Debugf("设备 %s WebSocket读取超时或连接关闭: %v", state.DeviceMAC, err)
			}
			break
		}

		if msgType == websocket.BinaryMessage {
			logger.Infof("设备 %s 收到WebSocket二进制音频消息，大小: %d bytes", state.DeviceMAC, len(message))
			state.handleWebSocketAudioPacket(message)
			continue
		} else {
			logger.Debugf("设备 %s 收到WebSocket文本消息: %s", state.DeviceMAC, string(message))

			// 解析消息
			var wsMsg WebSocketMessagePayload
			if err := json.Unmarshal(message, &wsMsg); err != nil {
				logger.Errorf("设备 %s 解析WebSocket消息失败: %v", state.DeviceMAC, err)
				continue
			}

			logger.Infof("设备 %s 解析WebSocket消息成功，类型: %s", state.DeviceMAC, wsMsg.Type)

			// 处理消息
			handleWebSocketMessage(state, &wsMsg)
		}

	}

	logger.Infof("设备 %s WebSocket消息接收协程结束", state.DeviceMAC)
}

// 处理WebSocket消息
func handleWebSocketMessage(state *WebSocketTestState, msg *WebSocketMessagePayload) {
	logger.Debugf("设备 %s 收到WebSocket消息: %s", state.DeviceMAC)

	switch msg.Type {
	case "hello":
		if msg.SessionID != "" {
			state.SessionID = msg.SessionID
			logger.Infof("设备 %s 收到hello响应，SessionID: %s", state.DeviceMAC, state.SessionID)
		}
	case "tts":
		if msg.State == "start" {
			logger.Infof("设备 %s 收到tts_start消息", state.DeviceMAC)
		} else if msg.State == "stop" {
			logger.Infof("设备 %s 收到tts_stop消息", state.DeviceMAC)
		} else if msg.State == "sentence_start" {
			logger.Infof("设备 %s 收到tts_sentence_start消息", state.DeviceMAC)
		}
	case "llm":
		logger.Infof("设备 %s 收到llm消息", state.DeviceMAC)
	case "iot":
		logger.Infof("设备 %s 收到iot消息", state.DeviceMAC)
	case "end_chat":
		logger.Debugf("设备 %s 收到end_chat响应", state.DeviceMAC)
	case "save_audio":
		logger.Infof("设备 %s 收到save_audio消息", state.DeviceMAC)
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

			strPath, err := OpusToWav(nil, audioData, state.SessionID+"____"+cast.ToString(len(audioData)))
			if err != nil {
				logger.Errorf("设备 %s 转换音频数据为WAV文件失败: %v", state.SessionID, err)
			} else {
				logger.Debugf("设备 %s 转换音频数据为WAV文件成功,路径: %s", state.SessionID, strPath)
			}

			state.CloseSignal <- struct{}{}
		}()

	case "goodbye":
		logger.Infof("设备 %s 收到goodbye响应", state.DeviceMAC)
	}
}

// 处理WebSocket音频包
func (s *WebSocketTestState) handleWebSocketAudioPacket(audioData []byte) {
	// logger.Infof("设备 %s 收到WebSocket音频包，大小: %d bytes", s.SessionID, len(audioData))

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
	audioDataCopy := make([]byte, len(audioData))
	copy(audioDataCopy, audioData)

	s.AudioFrames = append(s.AudioFrames, audioDataCopy)
	currentCount := len(s.AudioFrames)
	s.AudioFramesMutex.Unlock()

	logger.Infof("设备 %s 音频包已存储，当前总音频包数: %d", s.SessionID, currentCount)
}

// 发送WebSocket消息
func sendWebSocketMessage(state *WebSocketTestState, msg *WebSocketMessagePayload) error {
	if !state.Connected || state.WebSocketConn == nil {
		return fmt.Errorf("WebSocket未连接")
	}

	// 序列化消息
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("序列化WebSocket消息失败: %v", err)
	}

	// 发送消息
	err = state.WebSocketConn.WriteMessage(websocket.TextMessage, data)
	if err != nil {
		return fmt.Errorf("发送WebSocket消息失败: %v", err)
	}

	logger.Debugf("设备 %s 发送WebSocket消息: %s", state.DeviceMAC, msg.Type)
	return nil
}

func sendWebAudioData(state *WebSocketTestState, audioData []byte) error {

	// 发送消息
	err := state.WebSocketConn.WriteMessage(websocket.BinaryMessage, audioData)
	if err != nil {
		return fmt.Errorf("发送WebSocket消息失败: %v", err)
	}
	return nil
}

// WebSocket IoT消息测试
func testWebSocketIoT(state *WebSocketTestState, device DeviceInfo) error {
	logger.Infof("设备 %s 发送WebSocket IoT消息...", device.MacAddress)

	// 发送IoT消息
	iotMsg := &WebSocketMessagePayload{
		Type:      "iot",
		SessionID: state.SessionID,
		Update:    true,
		States: []*States{
			{
				Name: "Speaker",
				State: map[string]interface{}{
					"volume": map[string]interface{}{
						"description": "当前音量值",
						"type":        "number",
					},
				},
				// Commands: []*Command{
				// 	{
				// 		Name: "SetVolume",
				// 		Method: "set_volume",
				// 		Parameters: map[string]interface{}{
				// 			"volume": map[string]interface{}{
				// 				"description": "0到100之间的整数",
				// 				"type":        "number",
				// 			},
				// 		},
				// 	},
				// },
			},
		},
	}

	if err := sendWebSocketMessage(state, iotMsg); err != nil {
		return fmt.Errorf("发送IoT消息失败: %v", err)
	}

	// 发送设备状态
	stateMsg := &WebSocketMessagePayload{
		Type:      "iot",
		SessionID: state.SessionID,
		Update:    true,
		States: []*States{
			{
				Name: "Speaker",
				State: map[string]interface{}{
					"volume": map[string]interface{}{
						"description": "当前音量值",
						"type":        "number",
					},
				},
			},
			{
				Name: "Battery",
				State: map[string]interface{}{
					"level":    80,
					"charging": false,
				},
			},
		},
	}

	if err := sendWebSocketMessage(state, stateMsg); err != nil {
		return fmt.Errorf("发送设备状态失败: %v", err)
	}

	logger.Infof("设备 %s WebSocket IoT消息发送成功", device.MacAddress)
	return nil
}

// WebSocket listen start消息测试
func testWebSocketListenStart(state *WebSocketTestState, device DeviceInfo) error {
	logger.Infof("设备 %s 发送WebSocket listen start消息...", device.MacAddress)

	listenMsg := &WebSocketMessagePayload{
		Type:      "listen",
		SessionID: state.SessionID,
		Mode:      "manual",
		State:     "start",
	}

	if err := sendWebSocketMessage(state, listenMsg); err != nil {
		return fmt.Errorf("发送listen start消息失败: %v", err)
	}

	logger.Infof("设备 %s WebSocket listen start消息发送成功", device.MacAddress)
	return nil
}

// WebSocket音频传输测试
func testWebSocketAudio(state *WebSocketTestState, wavFile string) error {
	logger.Infof("开始WebSocket音频传输，使用文件: %s", wavFile)

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
		// audioPacket, err := state.buildWebSocketAudioPacket(frame)
		// if err != nil {
		// 	return fmt.Errorf("构建音频包失败: %v", err)
		// }

		// 发送音频包
		if err := sendWebAudioData(state, frame); err != nil {
			return fmt.Errorf("发送音频包失败: %v", err)
		}

		// logger.Debugf("发送音频包 %d/%d，大小: %d bytes,sessionID: %s, 时间戳: %d",
		// 	i+1, len(opusFrames), len(frame), state.SessionID, time.Now().UnixMilli())

		// 模拟实时发送间隔
		time.Sleep(60 * time.Millisecond) // 60ms帧间隔
	}

	logger.Info("WebSocket音频传输完成")
	return nil
}

// 构建WebSocket音频包
func (s *WebSocketTestState) buildWebSocketAudioPacket(audioData []byte) (*WebSocketMessagePayload, error) {
	// 构建音频包
	audioMsg := &WebSocketMessagePayload{
		Type:      "audio",
		SessionID: s.SessionID,
		Version:   3,
		Transport: "websocket",
		AudioParams: &AudioParams{
			Format:        "opus",
			SampleRate:    16000,
			Channels:      1,
			FrameDuration: 60,
		},
	}
	// 递增序列号
	s.Sequence++

	return audioMsg, nil
}

// WebSocket listen stop消息测试
func testWebSocketListenStop(state *WebSocketTestState, device DeviceInfo) error {
	logger.Infof("设备 %s 发送WebSocket listen stop消息...", device.MacAddress)

	listenMsg := &WebSocketMessagePayload{
		Type:      "listen",
		SessionID: state.SessionID,
		State:     "stop",
	}

	if err := sendWebSocketMessage(state, listenMsg); err != nil {
		return fmt.Errorf("发送listen stop消息失败: %v", err)
	}

	// 记录listen stop发送时间
	state.ListenStopTime = time.Now()
	logger.Infof("设备 %s WebSocket listen stop消息发送成功, 时间: %v", state.SessionID, state.ListenStopTime)
	return nil
}

// WebSocket文本消息测试
func testWebSocketText(state *WebSocketTestState, device DeviceInfo) error {
	logger.Infof("设备 %s 发送WebSocket文本消息...", device.MacAddress)

	textMsg := &WebSocketMessagePayload{
		Type:      "text",
		SessionID: state.SessionID,
		Text:      "你好，这是一个测试消息",
	}

	if err := sendWebSocketMessage(state, textMsg); err != nil {
		return fmt.Errorf("发送文本消息失败: %v", err)
	}

	// 等待响应
	time.Sleep(WaitForHelloTime * time.Second)

	logger.Infof("设备 %s WebSocket文本消息发送成功", device.MacAddress)
	return nil
}

// WebSocket goodbye测试
func testWebSocketGoodbye(state *WebSocketTestState, device DeviceInfo) error {
	logger.Infof("设备 %s 发送WebSocket goodbye消息...", device.MacAddress)

	goodbyeMsg := &WebSocketMessagePayload{
		Type:      "goodbye",
		SessionID: state.SessionID,
	}

	if err := sendWebSocketMessage(state, goodbyeMsg); err != nil {
		return fmt.Errorf("发送goodbye消息失败: %v", err)
	}

	// 等待goodbye响应
	time.Sleep(WaitForGoodbyeTime * time.Second)

	logger.Infof("设备 %s WebSocket goodbye消息发送成功,sessionID: %s", device.MacAddress, state.SessionID)
	return nil
}

// 清理WebSocket资源
func cleanupWebSocket(state *WebSocketTestState) {
	logger.Info("清理WebSocket资源...")

	if state.WebSocketConn != nil {
		state.WebSocketConn.Close()
	}

	state.Connected = false
	logger.Info("WebSocket资源清理完成")
}

// 添加WebSocket测试结果
func addWebSocketTestResult(result TestResult) {
	websocketTestResultsLock.Lock()
	defer websocketTestResultsLock.Unlock()
	websocketTestResults = append(websocketTestResults, result)
}

// 打印WebSocket测试结果统计
func printWebSocketTestResults() {
	websocketTestResultsLock.Lock()
	defer websocketTestResultsLock.Unlock()

	logger.Info("=== WebSocket测试结果统计 ===")

	totalTests := len(websocketTestResults)
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

	for _, result := range websocketTestResults {
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
	for _, result := range websocketTestResults {
		if !result.Success {
			logger.Errorf("设备 %s WebSocket测试失败: %s", result.DeviceMAC, result.ErrorMessage)
		}
	}

	// 保存详细结果到JSON文件
	saveWebSocketDetailedResults()
}

// 保存WebSocket详细结果到JSON文件
func saveWebSocketDetailedResults() {
	data, err := json.MarshalIndent(websocketTestResults, "", "  ")
	if err != nil {
		logger.Errorf("序列化WebSocket测试结果失败: %v", err)
		return
	}

	filename := fmt.Sprintf("statics/test_json/websocket_test_results_%s.json", time.Now().Format("20060102_150405"))
	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		logger.Errorf("保存WebSocket测试结果失败: %v", err)
		return
	}

	logger.Infof("WebSocket详细测试结果已保存到: %s", filename)
}

// WebSocket连续对话测试主函数
func RunWebSocketContinuousDialogueTest() {
	logger.Info("开始WebSocket连续对话测试...")

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

	logger.Infof("准备测试 %d 个设备的WebSocket连续对话", len(devices))

	// 启动并发测试
	var wg sync.WaitGroup
	startTime := time.Now()

	for i, device := range devices {
		wg.Add(1)
		logger.Infof("启动设备 %d WebSocket连续对话测试: %s", i+1, device.MacAddress)
		go runSingleDeviceWebSocketContinuousDialogue(device, &wg)

		// 稍微延迟启动，避免同时连接过多
		time.Sleep(100 * time.Millisecond)
	}

	// 等待所有测试完成
	wg.Wait()

	totalTime := time.Since(startTime)
	logger.Infof("所有设备WebSocket连续对话测试完成，总耗时: %v", totalTime)

	// 打印测试结果统计
	printWebSocketContinuousDialogueResults()

	logger.Info("WebSocket连续对话测试完成")
}

// 单个设备WebSocket连续对话测试
func runSingleDeviceWebSocketContinuousDialogue(device DeviceInfo, wg *sync.WaitGroup) {
	defer wg.Done()

	result := ContinuousDialogueResult{
		DeviceMAC:    device.MacAddress,
		StartTime:    time.Now(),
		Success:      false,
		RoundResults: make([]DialogueRoundResult, 0),
	}

	logger.Infof("开始设备WebSocket连续对话测试: %s", device.MacAddress)

	// 初始化WebSocket连续对话状态
	state := &WebSocketContinuousDialogueState{
		WebSocketTestState: &WebSocketTestState{
			TestState: &TestState{
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
			},
			DeviceMAC:            device.MacAddress,
			AudioFrames:          make([][]byte, 0),
			FirstAudioReceived:   false,
			MessageChan:          make(chan WebSocketMessage, 100),
			Connected:            false,
			ReconnectAttempts:    0,
			MaxReconnectAttempts: 3,
		},
		RoundNumber:    0,
		RoundResults:   make([]DialogueRoundResult, 0),
		TotalStartTime: time.Now(),
	}

	// 1. HTTP认证
	authResp, err := testHTTPAuthWithDevice(state.TestState, device)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("HTTP认证失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		addWebSocketContinuousDialogueResult(result)
		logger.Errorf("设备 %s HTTP认证失败: %v", device.MacAddress, err)
		return
	}

	// 2. WebSocket连接和hello握手
	if sessionID, err := testWebSocketHello(authResp, state.WebSocketTestState, device); err != nil {
		result.ErrorMessage = fmt.Sprintf("WebSocket hello握手失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		addWebSocketContinuousDialogueResult(result)
		logger.Errorf("设备 %s WebSocket hello握手失败: %v", device.MacAddress, err)
		return
	} else {
		state.SessionID = sessionID
		result.SessionID = sessionID
	}

	// 3. 发送IoT消息
	if err := testWebSocketIoT(state.WebSocketTestState, device); err != nil {
		result.ErrorMessage = fmt.Sprintf("WebSocket IoT消息失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		addWebSocketContinuousDialogueResult(result)
		logger.Errorf("设备 %s WebSocket IoT消息失败: %v", device.MacAddress, err)
		return
	}

	// 4. 执行连续对话
	for round := 1; round <= ContinuousDialogueCount; round++ {
		logger.Infof("设备 %s 开始第 %d 轮WebSocket对话", device.MacAddress, round)

		roundResult := executeWebSocketDialogueRound(state, device, round)
		result.RoundResults = append(result.RoundResults, roundResult)

		if roundResult.Success {
			result.SuccessRounds++
		} else {
			result.FailedRounds++
			logger.Errorf("设备 %s 第 %d 轮WebSocket对话失败: %s", device.MacAddress, round, roundResult.ErrorMessage)
		}

		// 等待一段时间再进行下一轮
		time.Sleep(2 * time.Second)
	}

	// 5. 等待所有轮次完成后再发送goodbye消息
	time.Sleep(5 * time.Second)
	if err := testWebSocketGoodbye(state.WebSocketTestState, device); err != nil {
		logger.Errorf("设备 %s WebSocket goodbye失败: %v", device.MacAddress, err)
	}

	// 等待测试完成
	time.Sleep(WaitForEndTime * time.Second)

	// 清理资源
	cleanupWebSocket(state.WebSocketTestState)

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.TotalRounds = ContinuousDialogueCount
	result.Success = result.SuccessRounds > 0

	addWebSocketContinuousDialogueResult(result)
	logger.Infof("设备 %s WebSocket连续对话测试完成，成功轮数: %d/%d", device.MacAddress, result.SuccessRounds, result.TotalRounds)
}

// 执行WebSocket单轮对话
func executeWebSocketDialogueRound(state *WebSocketContinuousDialogueState, device DeviceInfo, roundNumber int) DialogueRoundResult {
	roundResult := DialogueRoundResult{
		RoundNumber: roundNumber,
		StartTime:   time.Now(),
		Success:     false,
	}

	logger.Infof("设备 %s 第 %d 轮WebSocket对话开始", device.MacAddress, roundNumber)

	// 重置音频接收状态
	state.FirstAudioReceived = false
	state.AudioFrames = make([][]byte, 0)
	state.LastAudioReceiveTime = time.Time{} // 重置最后音频接收时间

	// 1. 发送listen start消息
	listenStartTime := time.Now()
	if err := testWebSocketListenStart(state.WebSocketTestState, device); err != nil {
		roundResult.ErrorMessage = fmt.Sprintf("WebSocket listen start消息失败: %v", err)
		roundResult.EndTime = time.Now()
		roundResult.Duration = roundResult.EndTime.Sub(roundResult.StartTime)
		roundResult.ListenStartTime = roundResult.EndTime.Sub(listenStartTime)
		return roundResult
	}
	roundResult.ListenStartTime = time.Since(listenStartTime)

	// 等待一段时间让服务器准备
	time.Sleep(WaitForSendAudioTime)

	// 2. WebSocket音频传输
	audioSendTime := time.Now()
	wavFile := getRandomWavFile()
	if err := testWebSocketAudio(state.WebSocketTestState, wavFile); err != nil {
		roundResult.ErrorMessage = fmt.Sprintf("WebSocket音频传输失败: %v", err)
		roundResult.EndTime = time.Now()
		roundResult.Duration = roundResult.EndTime.Sub(roundResult.StartTime)
		roundResult.AudioSendTime = roundResult.EndTime.Sub(audioSendTime)
		return roundResult
	}
	roundResult.AudioSendTime = time.Since(audioSendTime)

	// 3. 发送listen stop消息
	listenStopTime := time.Now()
	if err := testWebSocketListenStop(state.WebSocketTestState, device); err != nil {
		roundResult.ErrorMessage = fmt.Sprintf("WebSocket listen stop消息失败: %v", err)
		roundResult.EndTime = time.Now()
		roundResult.Duration = roundResult.EndTime.Sub(roundResult.StartTime)
		roundResult.ListenStopTime = roundResult.EndTime.Sub(listenStopTime)
		return roundResult
	}
	roundResult.ListenStopTime = time.Since(listenStopTime)

	// 4. 等待接收音频响应并监控音频接收情况
	waitStartTime := time.Now()
	timeout := 30 * time.Second // 总体超时时间
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var lastAudioCount int = 0
	var audioReceiveTimeout time.Duration = 3 * time.Second // 3秒内没收到音频表示接收结束

	logger.Infof("设备 %s 第 %d 轮WebSocket对话开始等待音频响应...", device.MacAddress, roundNumber)

	for {
		select {
		case <-state.CloseSignal:
			logger.Infof("设备 %s 第 %d 轮WebSocket对话收到关闭信号，结束等待音频响应", device.MacAddress, roundNumber)
			return roundResult
		case <-ticker.C:
			// 检查是否收到音频
			state.AudioFramesMutex.Lock()
			currentAudioCount := len(state.AudioFrames)
			state.AudioFramesMutex.Unlock()

			// 每10次检查打印一次调试信息
			if (time.Since(waitStartTime).Milliseconds()/100)%10 == 0 {
				logger.Debugf("设备 %s 第 %d 轮WebSocket对话等待音频中，当前音频包数: %d, 等待时间: %v",
					device.MacAddress, roundNumber, currentAudioCount, time.Since(waitStartTime))
			}

			if currentAudioCount > 0 {
				// 记录第一个音频包的时间
				if !state.FirstAudioReceived {
					state.FirstAudioReceived = true
					roundResult.FirstAudioTime = time.Since(waitStartTime)
					logger.Infof("设备 %s 第 %d 轮WebSocket对话收到第一个音频包，时间差: %v",
						device.MacAddress, roundNumber, roundResult.FirstAudioTime)
				}

				// 如果音频包数量增加，更新最后接收时间
				if currentAudioCount > lastAudioCount {
					state.LastAudioReceiveTime = time.Now()
					lastAudioCount = currentAudioCount
					logger.Infof("设备 %s 第 %d 轮WebSocket对话收到音频包，当前数量: %d",
						device.MacAddress, roundNumber, currentAudioCount)
				}

				// 检查是否3秒内没有收到新音频
				if !state.LastAudioReceiveTime.IsZero() &&
					time.Since(state.LastAudioReceiveTime) >= audioReceiveTimeout {

					logger.Infof("设备 %s 第 %d 轮WebSocket对话音频接收结束，3秒内无新音频，总音频包数: %d",
						device.MacAddress, roundNumber, currentAudioCount)

					// 发送save_audio消息
					if err := testWebSocketSaveAudio(state.WebSocketTestState, device); err != nil {
						logger.Errorf("设备 %s 第 %d 轮WebSocket对话发送save_audio消息失败: %v", device.MacAddress, roundNumber, err)
					} else {
						logger.Infof("设备 %s 第 %d 轮WebSocket对话发送save_audio消息成功", device.MacAddress, roundNumber)
					}

					// 等待save_audio响应（音频保存完成）
					saveAudioTimeout := 10 * time.Second
					saveAudioTicker := time.NewTicker(100 * time.Millisecond)
					defer saveAudioTicker.Stop()

					for {
						select {
						case <-saveAudioTicker.C:
							// 检查是否收到save_audio响应
							state.AudioFramesMutex.Lock()
							// 这里可以添加检查save_audio响应状态的逻辑
							state.AudioFramesMutex.Unlock()

							// 简单等待一段时间让音频保存完成
							time.Sleep(2 * time.Second)

							roundResult.AudioPackets = currentAudioCount
							roundResult.EndTime = time.Now()
							roundResult.Duration = roundResult.EndTime.Sub(roundResult.StartTime)
							roundResult.Success = true

							logger.Infof("设备 %s 第 %d 轮WebSocket对话成功，收到 %d 个音频包，耗时: %v",
								device.MacAddress, roundNumber, currentAudioCount, roundResult.Duration)
							return roundResult
						case <-time.After(saveAudioTimeout):
							logger.Errorf("设备 %s 第 %d 轮WebSocket对话等待save_audio响应超时", device.MacAddress, roundNumber)
							roundResult.ErrorMessage = "等待save_audio响应超时"
							roundResult.EndTime = time.Now()
							roundResult.Duration = roundResult.EndTime.Sub(roundResult.StartTime)
							return roundResult
						}
					}
				}
			}
		case <-time.After(timeout):
			roundResult.ErrorMessage = "等待音频响应超时"
			roundResult.EndTime = time.Now()
			roundResult.Duration = roundResult.EndTime.Sub(roundResult.StartTime)
			roundResult.FirstAudioTime = time.Since(waitStartTime)

			logger.Errorf("设备 %s 第 %d 轮WebSocket对话超时，未收到任何音频数据", device.MacAddress, roundNumber)
			return roundResult
		}
	}
}

// WebSocket save_audio消息测试
func testWebSocketSaveAudio(state *WebSocketTestState, device DeviceInfo) error {
	logger.Infof("设备 %s 发送WebSocket save_audio消息...", device.MacAddress)

	saveAudioMsg := &WebSocketMessagePayload{
		Type:      "save_audio",
		SessionID: state.SessionID,
	}

	if err := sendWebSocketMessage(state, saveAudioMsg); err != nil {
		return fmt.Errorf("发送save_audio消息失败: %v", err)
	}

	logger.Infof("设备 %s WebSocket save_audio消息发送成功,sessionID: %s", device.MacAddress, state.SessionID)
	return nil
}

// 全局WebSocket连续对话结果收集器
var (
	websocketContinuousDialogueResults     []ContinuousDialogueResult
	websocketContinuousDialogueResultsLock sync.Mutex
)

// 添加WebSocket连续对话测试结果
func addWebSocketContinuousDialogueResult(result ContinuousDialogueResult) {
	websocketContinuousDialogueResultsLock.Lock()
	defer websocketContinuousDialogueResultsLock.Unlock()
	websocketContinuousDialogueResults = append(websocketContinuousDialogueResults, result)
}

// 打印WebSocket连续对话测试结果统计
func printWebSocketContinuousDialogueResults() {
	websocketContinuousDialogueResultsLock.Lock()
	defer websocketContinuousDialogueResultsLock.Unlock()

	logger.Info("=== WebSocket连续对话测试结果统计 ===")

	totalTests := len(websocketContinuousDialogueResults)
	successTests := 0
	var totalDuration time.Duration
	var totalRounds int
	var successRounds int
	var failedRounds int

	for _, result := range websocketContinuousDialogueResults {
		if result.Success {
			successTests++
			totalDuration += result.Duration
			totalRounds += result.TotalRounds
			successRounds += result.SuccessRounds
			failedRounds += result.FailedRounds
		}
	}

	logger.Infof("总测试数: %d", totalTests)
	logger.Infof("成功测试数: %d", successTests)
	logger.Infof("成功率: %.2f%%", float64(successTests)/float64(totalTests)*100)

	if successTests > 0 {
		logger.Infof("平均总耗时: %v", totalDuration/time.Duration(successTests))
		logger.Infof("总对话轮数: %d", totalRounds)
		logger.Infof("成功对话轮数: %d", successRounds)
		logger.Infof("失败对话轮数: %d", failedRounds)
		logger.Infof("对话成功率: %.2f%%", float64(successRounds)/float64(totalRounds)*100)
	}

	// 打印失败的测试
	for _, result := range websocketContinuousDialogueResults {
		if !result.Success {
			logger.Errorf("设备 %s WebSocket连续对话测试失败: %s", result.DeviceMAC, result.ErrorMessage)
		}
	}

	// 保存详细结果到JSON文件
	saveWebSocketContinuousDialogueResults()
}

// 保存WebSocket连续对话详细结果到JSON文件
func saveWebSocketContinuousDialogueResults() {
	data, err := json.MarshalIndent(websocketContinuousDialogueResults, "", "  ")
	if err != nil {
		logger.Errorf("序列化WebSocket连续对话测试结果失败: %v", err)
		return
	}

	filename := fmt.Sprintf("statics/test_json/websocket_continuous_dialogue_results_%s.json", time.Now().Format("20060102_150405"))
	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		logger.Errorf("保存WebSocket连续对话测试结果失败: %v", err)
		return
	}

	logger.Infof("WebSocket连续对话详细测试结果已保存到: %s", filename)
}

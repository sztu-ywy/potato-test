package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/uozi-tech/cosy/logger"
)

// 连续对话测试结果
type ContinuousDialogueResult struct {
	DeviceMAC     string                `json:"device_mac"`
	SessionID     string                `json:"session_id"`
	StartTime     time.Time             `json:"start_time"`
	EndTime       time.Time             `json:"end_time"`
	Duration      time.Duration         `json:"duration"`
	TotalRounds   int                   `json:"total_rounds"`
	SuccessRounds int                   `json:"success_rounds"`
	FailedRounds  int                   `json:"failed_rounds"`
	RoundResults  []DialogueRoundResult `json:"round_results"`
	Success       bool                  `json:"success"`
	ErrorMessage  string                `json:"error_message"`
}

// 单轮对话结果
type DialogueRoundResult struct {
	RoundNumber     int           `json:"round_number"`
	StartTime       time.Time     `json:"start_time"`
	EndTime         time.Time     `json:"end_time"`
	Duration        time.Duration `json:"duration"`
	ListenStartTime time.Duration `json:"listen_start_time"`
	AudioSendTime   time.Duration `json:"audio_send_time"`
	ListenStopTime  time.Duration `json:"listen_stop_time"`
	FirstAudioTime  time.Duration `json:"first_audio_time"`
	AudioPackets    int           `json:"audio_packets"`
	Success         bool          `json:"success"`
	ErrorMessage    string        `json:"error_message"`
}

// 连续对话状态
type ContinuousDialogueState struct {
	*TestState
	RoundNumber       int
	RoundResults      []DialogueRoundResult
	CurrentRoundStart time.Time
	TotalStartTime    time.Time
}

// 全局连续对话结果收集器
var (
	continuousDialogueResults     []ContinuousDialogueResult
	continuousDialogueResultsLock sync.Mutex
)

// 连续对话测试主函数
func runContinuousDialogueTest() {
	logger.Info("开始连续对话测试...")

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

	logger.Infof("准备测试 %d 个设备的连续对话", len(devices))

	// 启动并发测试
	var wg sync.WaitGroup
	startTime := time.Now()

	for i, device := range devices {
		wg.Add(1)
		logger.Infof("启动设备 %d 连续对话测试: %s", i+1, device.MacAddress)
		go runSingleDeviceContinuousDialogue(device, &wg)

		// 稍微延迟启动，避免同时连接过多
		time.Sleep(100 * time.Millisecond)
	}

	// 等待所有测试完成
	wg.Wait()

	totalTime := time.Since(startTime)
	logger.Infof("所有设备连续对话测试完成，总耗时: %v", totalTime)

	// 打印测试结果统计
	printContinuousDialogueResults()

	logger.Info("连续对话测试完成")
}

// 单个设备连续对话测试
func runSingleDeviceContinuousDialogue(device DeviceInfoRequest, wg *sync.WaitGroup) {
	defer wg.Done()

	result := ContinuousDialogueResult{
		DeviceMAC:    device.MacAddress,
		StartTime:    time.Now(),
		Success:      false,
		RoundResults: make([]DialogueRoundResult, 0),
	}

	logger.Infof("开始设备连续对话测试: %s", device.MacAddress)

	// 初始化连续对话状态
	state := &ContinuousDialogueState{
		TestState: &TestState{
			Sequence:           1,
			ReceivedText:       make([]string, 0),
			ReceivedAudio:      make([]int, 0),
			CommonPushTopic:    "",
			SubServerTopic:     GetServerTopic(device.MacAddress),
			AcceptAudioPacket:  &AudioPacket{},
			MQTT:               MQTT{},
			UDP:                UDP{},
			AudioFrames:        make([][]byte, 0),
			FirstAudioReceived: false,
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
		addContinuousDialogueResult(result)
		logger.Errorf("设备 %s HTTP认证失败: %v", device.MacAddress, err)
		return
	}

	// 2. MQTT连接和hello握手
	if sessionID, err := testMQTTHelloWithDevice(authResp, state.TestState, device); err != nil {
		result.ErrorMessage = fmt.Sprintf("MQTT hello握手失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		addContinuousDialogueResult(result)
		logger.Errorf("设备 %s MQTT hello握手失败: %v", device.MacAddress, err)
		return
	} else {
		state.SessionID = sessionID
		result.SessionID = sessionID
	}

	// 3. 发送IoT消息
	if err := testMQTTIoTWithDevice(state.TestState, device); err != nil {
		result.ErrorMessage = fmt.Sprintf("MQTT IoT消息失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		addContinuousDialogueResult(result)
		logger.Errorf("设备 %s MQTT IoT消息失败: %v", device.MacAddress, err)
		return
	}

	// 4. 建立UDP连接
	if err := setupUDPConnection(state.TestState); err != nil {
		result.ErrorMessage = fmt.Sprintf("UDP连接建立失败: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		addContinuousDialogueResult(result)
		logger.Errorf("设备 %s UDP连接建立失败: %v", device.MacAddress, err)
		return
	}

	// 启动UDP客户端监听
	go testUDPClient(state.TestState)

	// 等待UDP客户端启动
	time.Sleep(500 * time.Millisecond)

	// 5. 执行连续对话
	for round := 1; round <= ContinuousDialogueCount; round++ {
		logger.Infof("设备 %s 开始第 %d 轮对话", device.MacAddress, round)

		roundResult := executeDialogueRound(state, device, round)
		result.RoundResults = append(result.RoundResults, roundResult)

		if roundResult.Success {
			result.SuccessRounds++
		} else {
			result.FailedRounds++
			logger.Errorf("设备 %s 第 %d 轮对话失败: %s", device.MacAddress, round, roundResult.ErrorMessage)
		}

		// 等待一段时间再进行下一轮
		time.Sleep(2 * time.Second)
	}

	// 6. 等待所有轮次完成后再发送goodbye消息
	time.Sleep(5 * time.Second)
	if err := testMQTTGoodbyeWithDevice(state.TestState, device); err != nil {
		logger.Errorf("设备 %s MQTT goodbye失败: %v", device.MacAddress, err)
	}

	// 等待测试完成
	time.Sleep(WaitForEndTime * time.Second)

	// 清理资源
	cleanup(state.TestState)

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.TotalRounds = ContinuousDialogueCount
	result.Success = result.SuccessRounds > 0

	addContinuousDialogueResult(result)
	logger.Infof("设备 %s 连续对话测试完成，成功轮数: %d/%d", device.MacAddress, result.SuccessRounds, result.TotalRounds)
}

// 执行单轮对话
func executeDialogueRound(state *ContinuousDialogueState, device DeviceInfoRequest, roundNumber int) DialogueRoundResult {
	roundResult := DialogueRoundResult{
		RoundNumber: roundNumber,
		StartTime:   time.Now(),
		Success:     false,
	}

	logger.Infof("设备 %s 第 %d 轮对话开始", device.MacAddress, roundNumber)

	// 重置音频接收状态
	state.FirstAudioReceived = false
	state.AudioFrames = make([][]byte, 0)
	state.LastAudioReceiveTime = time.Time{} // 重置最后音频接收时间

	// 1. 发送listen start消息
	listenStartTime := time.Now()
	if err := testMQQTTListenStartWithDevice(state.TestState, device); err != nil {
		roundResult.ErrorMessage = fmt.Sprintf("MQTT listen start消息失败: %v", err)
		roundResult.EndTime = time.Now()
		roundResult.Duration = roundResult.EndTime.Sub(roundResult.StartTime)
		roundResult.ListenStartTime = roundResult.EndTime.Sub(listenStartTime)
		return roundResult
	}
	roundResult.ListenStartTime = time.Since(listenStartTime)

	// 等待一段时间让服务器准备
	time.Sleep(WaitForSendAudioTime)

	// 2. UDP音频传输
	audioSendTime := time.Now()
	wavFile := getRandomWavFile()
	if err := testUDPAudioWithFile(state.TestState, wavFile); err != nil {
		roundResult.ErrorMessage = fmt.Sprintf("UDP音频传输失败: %v", err)
		roundResult.EndTime = time.Now()
		roundResult.Duration = roundResult.EndTime.Sub(roundResult.StartTime)
		roundResult.AudioSendTime = roundResult.EndTime.Sub(audioSendTime)
		return roundResult
	}
	roundResult.AudioSendTime = time.Since(audioSendTime)

	// 3. 发送listen stop消息
	listenStopTime := time.Now()
	if err := testMQQTTListenStopWithDevice(state.TestState, device); err != nil {
		roundResult.ErrorMessage = fmt.Sprintf("MQTT listen stop消息失败: %v", err)
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
	var audioReceiveTimeout time.Duration = 3 * time.Second // 2秒内没收到音频表示接收结束

	for {
		select {
		case <-ticker.C:
			// 检查是否收到音频
			state.AudioFramesMutex.Lock()
			currentAudioCount := len(state.AudioFrames)
			state.AudioFramesMutex.Unlock()

			if currentAudioCount > 0 {
				// 记录第一个音频包的时间
				if !state.FirstAudioReceived {
					state.FirstAudioReceived = true
					roundResult.FirstAudioTime = time.Since(waitStartTime)
					logger.Infof("设备 %s 第 %d 轮对话收到第一个音频包，时间差: %v",
						device.MacAddress, roundNumber, roundResult.FirstAudioTime)
				}

				// 如果音频包数量增加，更新最后接收时间
				if currentAudioCount > lastAudioCount {
					state.LastAudioReceiveTime = time.Now()
					lastAudioCount = currentAudioCount
					logger.Debugf("设备 %s 第 %d 轮对话收到音频包，当前数量: %d",
						device.MacAddress, roundNumber, currentAudioCount)
				}

				// 检查是否2秒内没有收到新音频
				if !state.LastAudioReceiveTime.IsZero() &&
					time.Since(state.LastAudioReceiveTime) >= audioReceiveTimeout {

					logger.Infof("设备 %s 第 %d 轮对话音频接收结束，2秒内无新音频，总音频包数: %d",
						device.MacAddress, roundNumber, currentAudioCount)

					// 发送save_audio消息
					if err := testMQTTSaveAudioWithDevice(state.TestState, device); err != nil {
						logger.Errorf("设备 %s 第 %d 轮对话发送save_audio消息失败: %v", device.MacAddress, roundNumber, err)
					} else {
						logger.Infof("设备 %s 第 %d 轮对话发送save_audio消息成功", device.MacAddress, roundNumber)
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

							logger.Infof("设备 %s 第 %d 轮对话成功，收到 %d 个音频包，耗时: %v",
								device.MacAddress, roundNumber, currentAudioCount, roundResult.Duration)
							return roundResult
						case <-time.After(saveAudioTimeout):
							logger.Errorf("设备 %s 第 %d 轮对话等待save_audio响应超时", device.MacAddress, roundNumber)
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

			logger.Errorf("设备 %s 第 %d 轮对话超时", device.MacAddress, roundNumber)
			return roundResult
		}
	}
}

// 添加连续对话测试结果
func addContinuousDialogueResult(result ContinuousDialogueResult) {
	continuousDialogueResultsLock.Lock()
	defer continuousDialogueResultsLock.Unlock()
	continuousDialogueResults = append(continuousDialogueResults, result)
}

// 打印连续对话测试结果统计
func printContinuousDialogueResults() {
	continuousDialogueResultsLock.Lock()
	defer continuousDialogueResultsLock.Unlock()

	logger.Info("=== 连续对话测试结果统计 ===")

	totalTests := len(continuousDialogueResults)
	successTests := 0
	var totalDuration time.Duration
	var totalRounds int
	var successRounds int
	var failedRounds int

	for _, result := range continuousDialogueResults {
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
	for _, result := range continuousDialogueResults {
		if !result.Success {
			logger.Errorf("设备 %s 连续对话测试失败: %s", result.DeviceMAC, result.ErrorMessage)
		}
	}

	// 保存详细结果到JSON文件
	saveContinuousDialogueResults()
}

// 保存连续对话详细结果到JSON文件
func saveContinuousDialogueResults() {
	data, err := json.MarshalIndent(continuousDialogueResults, "", "  ")
	if err != nil {
		logger.Errorf("序列化连续对话测试结果失败: %v", err)
		return
	}

	filename := fmt.Sprintf("statics/test_json/continuous_dialogue_results_%s.json", time.Now().Format("20060102_150405"))
	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		logger.Errorf("保存连续对话测试结果失败: %v", err)
		return
	}

	logger.Infof("连续对话详细测试结果已保存到: %s", filename)
}

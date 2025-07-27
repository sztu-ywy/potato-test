package main

// 测试客户端
// type TestClient struct {
// 	SessionID     string
// 	Nonce         [16]byte
// 	MQTTClient    mqtt.Client
// 	UDPConn       *net.UDPConn
// 	DeviceTopic   string
// 	ServerTopic   string
// 	Sequence      uint32
// 	ReceivedText  []string
// 	ReceivedAudio []int
// }

// // 测试配置

// func main() {
// 	logger.Info("开始UDP+MQTT集成测试...")

// 	client := &TestClient{
// 		Sequence:      1,
// 		ReceivedText:  make([]string, 0),
// 		ReceivedAudio: make([]int, 0),
// 	}

// 	// 1. HTTP认证
// 	if err := client.testHTTPAuth(); err != nil {
// 		logger.Errorf("HTTP认证失败: %v", err)
// 		os.Exit(1)
// 	}

// 	// 2. MQTT连接和hello握手
// 	if err := client.testMQTTHello(); err != nil {
// 		logger.Errorf("MQTT hello握手失败: %v", err)
// 		os.Exit(1)
// 	}

// 	// 3. 发送IoT消息
// 	if err := client.testMQTTIoT(); err != nil {
// 		logger.Errorf("MQTT IoT消息失败: %v", err)
// 		os.Exit(1)
// 	}

// 	// 4. 发送listen消息
// 	if err := client.testMQTTListen(); err != nil {
// 		logger.Errorf("MQTT listen消息失败: %v", err)
// 		os.Exit(1)
// 	}

// 	// 5. UDP音频传输
// 	if err := client.testUDPAudio(); err != nil {
// 		logger.Errorf("UDP音频传输失败: %v", err)
// 		os.Exit(1)
// 	}

// 	// 6. 发送文本消息
// 	if err := client.testMQTTText(); err != nil {
// 		logger.Errorf("MQTT文本消息失败: %v", err)
// 		os.Exit(1)
// 	}

// 	// 7. MQTT goodbye
// 	if err := client.testMQTTGoodbye(); err != nil {
// 		logger.Errorf("MQTT goodbye失败: %v", err)
// 		os.Exit(1)
// 	}

// 	// 8. 清理资源
// 	client.cleanup()

// 	logger.Info("集成测试完成")
// }

// // HTTP认证测试
// func (c *TestClient) testHTTPAuth() error {
// 	logger.Info("1. 开始HTTP认证...")

// 	// 构建认证请求
// 	authReq := device.DeviceInfo{
// 		Version:             "2",
// 		Language:            "zh-CN",
// 		FlashSize:           16777216,
// 		MinimumFreeHeapSize: 8441000,
// 		MacAddress:          DeviceMAC,
// 		UUID:                DeviceUUID,
// 		ChipModelName:       "esp32s3",
// 		Application: device.AppInfo{
// 			Name:        "xiaozhi",
// 			Version:     "2.1.0",
// 			CompileTime: "Jul 15 2025T15:28:46Z",
// 			IDFVersion:  "v5.3.1-dirty",
// 		},
// 		Board: device.BoardInfo{
// 			Type:     "tudouzi",
// 			Name:     "tudouzi",
// 			Revision: "ML307R-DL-MBRH0S01",
// 			Carrier:  "CHINA MOBILE",
// 			CSQ:      "26",
// 			IMEI:     "460240429759840",
// 			ICCID:    "89860842102481934840",
// 		},
// 	}

// 	// 发送HTTP请求
// 	jsonData, err := json.Marshal(authReq)
// 	if err != nil {
// 		return fmt.Errorf("序列化认证请求失败: %v", err)
// 	}

// 	resp, err := http.Post(
// 		fmt.Sprintf("http://%s:%d/devices/auth", ServerHost, ServerPort),
// 		"application/json",
// 		strings.NewReader(string(jsonData)),
// 	)
// 	if err != nil {
// 		return fmt.Errorf("HTTP请求失败: %v", err)
// 	}
// 	defer resp.Body.Close()

// 	if resp.StatusCode != http.StatusOK {
// 		return fmt.Errorf("HTTP认证失败，状态码: %d", resp.StatusCode)
// 	}

// 	// 解析响应
// 	var authResp device.AuthResponse
// 	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
// 		return fmt.Errorf("解析认证响应失败: %v", err)
// 	}

// 	logger.Infof("HTTP认证成功，MQTT端点: %s", authResp.MQTT.Endpoint)
// 	return nil
// }

// // MQTT hello握手测试
// func (c *TestClient) testMQTTHello() error {
// 	logger.Info("2. 开始MQTT hello握手...")

// 	// 生成sessionID
// 	c.SessionID = fmt.Sprintf("%d", time.Now().Unix())

// 	// 生成nonce
// 	c.generateNonce()

// 	// 构建hello消息
// 	helloMsg := map[string]interface{}{
// 		"type":      "hello",
// 		"version":   3,
// 		"transport": "udp",
// 		"audio_params": map[string]interface{}{
// 			"format":         "opus",
// 			"sample_rate":    16000,
// 			"channels":       1,
// 			"frame_duration": 60,
// 		},
// 		"device_info": map[string]interface{}{
// 			"mac": DeviceMAC,
// 		},
// 	}

// 	// 连接MQTT
// 	opts := mqtt.NewClientOptions()
// 	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", MQTTHost, MQTTPort))
// 	opts.SetClientID("test-device-" + DeviceMAC)
// 	opts.SetUsername("test")
// 	opts.SetPassword("test")

// 	c.MQTTClient = mqtt.NewClient(opts)
// 	if token := c.MQTTClient.Connect(); token.Wait() && token.Error() != nil {
// 		return fmt.Errorf("MQTT连接失败: %v", token.Error())
// 	}

// 	// 订阅公共主题
// 	if token := c.MQTTClient.Subscribe("device-server", 1, func(client mqtt.Client, msg mqtt.Message) {
// 		c.handleHelloResponse(msg)
// 	}); token.Wait() && token.Error() != nil {
// 		return fmt.Errorf("MQTT订阅失败: %v", token.Error())
// 	}

// 	// 发送hello消息
// 	helloData, _ := json.Marshal(helloMsg)
// 	if token := c.MQTTClient.Publish("device-server", 1, false, helloData); token.Wait() && token.Error() != nil {
// 		return fmt.Errorf("MQTT发布hello失败: %v", token.Error())
// 	}

// 	// 等待hello响应
// 	time.Sleep(2 * time.Second)

// 	if c.SessionID == "" {
// 		return fmt.Errorf("未收到hello响应")
// 	}

// 	logger.Infof("MQTT hello握手成功，SessionID: %s", c.SessionID)
// 	return nil
// }

// // 处理hello响应
// func (c *TestClient) handleHelloResponse(msg mqtt.Message) {
// 	var response map[string]interface{}
// 	if err := json.Unmarshal(msg.Payload(), &response); err != nil {
// 		logger.Errorf("解析hello响应失败: %v", err)
// 		return
// 	}

// 	if response["type"] == "hello" {
// 		if sessionID, ok := response["session_id"].(string); ok {
// 			c.SessionID = sessionID
// 			logger.Infof("收到hello响应，SessionID: %s", sessionID)
// 		}
// 	}
// }

// // MQTT IoT消息测试
// func (c *TestClient) testMQTTIoT() error {
// 	logger.Info("3. 发送MQTT IoT消息...")

// 	// 生成主题
// 	macHash := c.hashMAC(DeviceMAC)
// 	c.DeviceTopic = fmt.Sprintf("potato/%s/td", macHash)
// 	c.ServerTopic = fmt.Sprintf("potato/%s/server", macHash)

// 	// 订阅服务端主题
// 	if token := c.MQTTClient.Subscribe(c.ServerTopic, 1, func(client mqtt.Client, msg mqtt.Message) {
// 		c.handleServerMessage(msg)
// 	}); token.Wait() && token.Error() != nil {
// 		return fmt.Errorf("订阅服务端主题失败: %v", token.Error())
// 	}

// 	// 发送IoT消息
// 	iotMsg := map[string]interface{}{
// 		"session_id": c.SessionID,
// 		"type":       "iot",
// 		"update":     true,
// 		"descriptors": []map[string]interface{}{
// 			{
// 				"name":        "Speaker",
// 				"description": "扬声器",
// 				"properties": map[string]interface{}{
// 					"volume": map[string]interface{}{
// 						"description": "当前音量值",
// 						"type":        "number",
// 					},
// 				},
// 				"methods": map[string]interface{}{
// 					"SetVolume": map[string]interface{}{
// 						"description": "设置音量",
// 						"parameters": map[string]interface{}{
// 							"volume": map[string]interface{}{
// 								"description": "0到100之间的整数",
// 								"type":        "number",
// 							},
// 						},
// 					},
// 				},
// 			},
// 		},
// 	}

// 	iotData, _ := json.Marshal(iotMsg)
// 	if token := c.MQTTClient.Publish(c.DeviceTopic, 1, false, iotData); token.Wait() && token.Error() != nil {
// 		return fmt.Errorf("发布IoT消息失败: %v", token.Error())
// 	}

// 	// 发送设备状态
// 	stateMsg := map[string]interface{}{
// 		"session_id": c.SessionID,
// 		"type":       "iot",
// 		"update":     true,
// 		"states": []map[string]interface{}{
// 			{
// 				"name": "Speaker",
// 				"state": map[string]interface{}{
// 					"volume": 90,
// 				},
// 			},
// 			{
// 				"name": "Battery",
// 				"state": map[string]interface{}{
// 					"level":    80,
// 					"charging": false,
// 				},
// 			},
// 		},
// 	}

// 	stateData, _ := json.Marshal(stateMsg)
// 	if token := c.MQTTClient.Publish(c.DeviceTopic, 1, false, stateData); token.Wait() && token.Error() != nil {
// 		return fmt.Errorf("发布设备状态失败: %v", token.Error())
// 	}

// 	logger.Info("MQTT IoT消息发送成功")
// 	return nil
// }

// // MQTT listen消息测试
// func (c *TestClient) testMQTTListen() error {
// 	logger.Info("4. 发送MQTT listen消息...")

// 	listenMsg := map[string]interface{}{
// 		"session_id": c.SessionID,
// 		"type":       "listen",
// 		"state":      "start",
// 		"mode":       "manual",
// 	}

// 	listenData, _ := json.Marshal(listenMsg)
// 	if token := c.MQTTClient.Publish(c.DeviceTopic, 1, false, listenData); token.Wait() && token.Error() != nil {
// 		return fmt.Errorf("发布listen消息失败: %v", token.Error())
// 	}

// 	logger.Info("MQTT listen消息发送成功")
// 	return nil
// }

// // UDP音频传输测试
// func (c *TestClient) testUDPAudio() error {
// 	logger.Info("5. 开始UDP音频传输...")

// 	// 连接UDP服务器
// 	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", UDPHost, UDPPort))
// 	if err != nil {
// 		return fmt.Errorf("解析UDP地址失败: %v", err)
// 	}

// 	c.UDPConn, err = net.DialUDP("udp", nil, udpAddr)
// 	if err != nil {
// 		return fmt.Errorf("连接UDP服务器失败: %v", err)
// 	}

// 	// 生成模拟音频帧
// 	opusFrames := c.generateMockOpusFrames()

// 	logger.Infof("开始发送 %d 个音频帧", len(opusFrames))

// 	// 发送音频帧
// 	for i, frame := range opusFrames {
// 		// 构建音频包
// 		audioPacket := c.buildAudioPacket(frame)

// 		// 发送音频包
// 		_, err := c.UDPConn.Write(audioPacket)
// 		if err != nil {
// 			return fmt.Errorf("发送音频包失败: %v", err)
// 		}

// 		logger.Debugf("发送音频包 %d/%d，大小: %d bytes", i+1, len(opusFrames), len(audioPacket))

// 		// 模拟实时发送间隔
// 		time.Sleep(60 * time.Millisecond) // 60ms帧间隔
// 	}

// 	logger.Info("UDP音频传输完成")
// 	return nil
// }

// // MQTT文本消息测试
// func (c *TestClient) testMQTTText() error {
// 	logger.Info("6. 发送MQTT文本消息...")

// 	textMsg := map[string]interface{}{
// 		"session_id": c.SessionID,
// 		"type":       "text",
// 		"text":       "你好，这是一个测试消息",
// 		"time":       time.Now().UnixMilli(),
// 	}

// 	textData, _ := json.Marshal(textMsg)
// 	if token := c.MQTTClient.Publish(c.DeviceTopic, 1, false, textData); token.Wait() && token.Error() != nil {
// 		return fmt.Errorf("发布文本消息失败: %v", token.Error())
// 	}

// 	// 等待响应
// 	time.Sleep(2 * time.Second)

// 	logger.Info("MQTT文本消息发送成功")
// 	return nil
// }

// // MQTT goodbye测试
// func (c *TestClient) testMQTTGoodbye() error {
// 	logger.Info("7. 发送MQTT goodbye消息...")

// 	goodbyeMsg := map[string]interface{}{
// 		"type":       "goodbye",
// 		"session_id": c.SessionID,
// 	}

// 	goodbyeData, _ := json.Marshal(goodbyeMsg)
// 	if token := c.MQTTClient.Publish("device-server", 1, false, goodbyeData); token.Wait() && token.Error() != nil {
// 		return fmt.Errorf("发布goodbye消息失败: %v", token.Error())
// 	}

// 	// 等待goodbye响应
// 	time.Sleep(1 * time.Second)

// 	logger.Info("MQTT goodbye消息发送成功")
// 	return nil
// }

// // 处理服务端消息
// func (c *TestClient) handleServerMessage(msg mqtt.Message) {
// 	var response map[string]interface{}
// 	if err := json.Unmarshal(msg.Payload(), &response); err != nil {
// 		logger.Errorf("解析服务端消息失败: %v", err)
// 		return
// 	}

// 	msgType, _ := response["type"].(string)
// 	logger.Infof("收到服务端消息: %s", msgType)

// 	switch msgType {
// 	case "text":
// 		if text, ok := response["text"].(string); ok {
// 			c.ReceivedText = append(c.ReceivedText, text)
// 			logger.Infof("收到文本消息: %s", text)
// 		}
// 	case "goodbye":
// 		logger.Info("收到goodbye响应")
// 	}
// }

// // 生成nonce
// func (c *TestClient) generateNonce() {
// 	// nonce格式: 01 + timestamp(8字节) + sessionID(8字节) + 0000000
// 	timestamp := uint64(time.Now().Unix())

// 	// 填充nonce
// 	copy(c.Nonce[0:2], []byte{0x01, 0x00})                           // 标识符
// 	binary.BigEndian.PutUint64(c.Nonce[2:10], timestamp)             // 时间戳
// 	copy(c.Nonce[10:16], []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}) // 填充
// }

// // 构建音频包
// func (c *TestClient) buildAudioPacket(audioData []byte) []byte {
// 	// 包格式: [16字节nonce][4字节时间戳][4字节序列号][音频数据]
// 	packet := make([]byte, 16+4+4+len(audioData))

// 	// 复制nonce
// 	copy(packet[0:16], c.Nonce[:])

// 	// 时间戳
// 	timestamp := uint32(time.Now().Unix())
// 	binary.BigEndian.PutUint32(packet[16:20], timestamp)

// 	// 序列号
// 	binary.BigEndian.PutUint32(packet[20:24], c.Sequence)

// 	// 音频数据
// 	copy(packet[24:], audioData)

// 	// 递增序列号
// 	c.Sequence++

// 	return packet
// }

// // 生成模拟Opus帧
// func (c *TestClient) generateMockOpusFrames() [][]byte {
// 	frames := make([][]byte, 0)
// 	for i := 0; i < 50; i++ { // 生成50个模拟帧
// 		frame := make([]byte, 100+i%50) // 模拟不同大小的帧
// 		for j := range frame {
// 			frame[j] = byte(i + j) // 填充一些数据
// 		}
// 		frames = append(frames, frame)
// 	}
// 	return frames
// }

// // 哈希MAC地址
// func (c *TestClient) hashMAC(mac string) string {
// 	hash := md5.Sum([]byte(mac))
// 	return hex.EncodeToString(hash[:])
// }

// // 清理资源
// func (c *TestClient) cleanup() {
// 	logger.Info("8. 清理资源...")

// 	if c.MQTTClient != nil {
// 		c.MQTTClient.Disconnect(250)
// 	}

// 	if c.UDPConn != nil {
// 		c.UDPConn.Close()
// 	}

// 	logger.Info("资源清理完成")
// }

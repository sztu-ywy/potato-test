package main


import (
	"fmt"
	"net"
	"time"

	"github.com/uozi-tech/cosy/logger"
)

// UDPTestClient UDP测试客户端
type UDPTestClient struct {
	conn       *net.UDPConn
	serverAddr *net.UDPAddr
	deviceID   string
}

// NewUDPTestClient 创建UDP测试客户端
func NewUDPTestClient(serverHost string, serverPort int, deviceID string) (*UDPTestClient, error) {
	serverAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", serverHost, serverPort))
	if err != nil {
		return nil, fmt.Errorf("解析服务器地址失败: %w", err)
	}

	// 创建本地UDP连接
	localAddr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		return nil, fmt.Errorf("解析本地地址失败: %w", err)
	}

	conn, err := net.DialUDP("udp", localAddr, serverAddr)
	if err != nil {
		return nil, fmt.Errorf("连接UDP服务器失败: %w", err)
	}

	return &UDPTestClient{
		conn:       conn,
		serverAddr: serverAddr,
		deviceID:   deviceID,
	}, nil
}

// SendAudioData 发送音频数据
func (c *UDPTestClient) SendAudioData(audioData []byte) error {
	// 构造数据包：[设备ID(8字节)][音频数据]
	packet := make([]byte, 8+len(audioData))

	// 填充设备ID（不足8字节用空格填充）
	deviceIDBytes := []byte(c.deviceID)
	if len(deviceIDBytes) > 8 {
		deviceIDBytes = deviceIDBytes[:8]
	} else {
		// 用空格填充到8字节
		for i := len(deviceIDBytes); i < 8; i++ {
			deviceIDBytes = append(deviceIDBytes, ' ')
		}
	}

	copy(packet[:8], deviceIDBytes)
	copy(packet[8:], audioData)

	_, err := c.conn.Write(packet)
	if err != nil {
		return fmt.Errorf("发送音频数据失败: %w", err)
	}

	logger.Infof("设备 [%s] 发送音频数据，长度: %d", c.deviceID, len(audioData))
	return nil
}

// ReceiveResponse 接收服务器响应
func (c *UDPTestClient) ReceiveResponse() error {
	buffer := make([]byte, 4096)

	// 设置读取超时
	c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	n, _, err := c.conn.ReadFromUDP(buffer)
	if err != nil {
		return fmt.Errorf("接收响应失败: %w", err)
	}

	if n > 8 {
		// 解析响应：[设备ID(8字节)][音频数据]
		responseDeviceID := string(buffer[:8])
		audioData := buffer[8:n]

		logger.Infof("设备 [%s] 收到响应，长度: %d", responseDeviceID, len(audioData))
		logger.Debugf("音频数据前100字节: %v", audioData[:min(100, len(audioData))])
	} else {
		logger.Warnf("收到无效响应，长度: %d", n)
	}

	return nil
}

// StartReceiving 开始持续接收响应
func (c *UDPTestClient) StartReceiving() {
	logger.Infof("设备 [%s] 开始监听服务器响应", c.deviceID)

	for {
		err := c.ReceiveResponse()
		if err != nil {
			logger.Errorf("接收响应错误: %v", err)
			time.Sleep(1 * time.Second)
		}
	}
}

// Close 关闭连接
func (c *UDPTestClient) Close() error {
	return c.conn.Close()
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestUDPServer 测试UDP服务器功能
func TestUDPServer() {
	logger.Info("开始UDP服务器测试")

	// 创建测试客户端
	client, err := NewUDPTestClient("127.0.0.1", 8888, "TEST001")
	if err != nil {
		logger.Errorf("创建测试客户端失败: %v", err)
		return
	}
	defer client.Close()

	// 启动响应接收协程
	go client.StartReceiving()

	// 模拟发送音频数据
	for i := 0; i < 5; i++ {
		// 模拟音频数据（这里用随机数据代替）
		audioData := []byte(fmt.Sprintf("test_audio_data_%d", i))

		err := client.SendAudioData(audioData)
		if err != nil {
			logger.Errorf("发送音频数据失败: %v", err)
			continue
		}

		time.Sleep(2 * time.Second)
	}

	logger.Info("UDP服务器测试完成")
}

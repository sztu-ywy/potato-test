package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/uozi-tech/cosy/logger"
)

// 音频配置
// type AudioConfig struct {
// 	SampleRate    int
// 	Channels      int
// 	FrameDuration int // 毫秒
// }

// 测试WAV转Opus转换
func TestWavToOpusConversion(t *testing.T) {
	logger.Info("开始WAV转Opus转换测试...")

	// 测试配置
	const TestWavFile = "./test_audio.wav"

	// 检查测试音频文件
	if _, err := os.Stat(TestWavFile); os.IsNotExist(err) {
		logger.Warnf("测试音频文件不存在: %s，跳过转换测试", TestWavFile)
		t.Skip("测试音频文件不存在")
	}

	// 音频配置
	config := AudioConfig{
		SampleRate:    16000,
		Channels:      1,
		FrameDuration: 60,
	}

	// 转换WAV到Opus
	opusFrames, err := ConvertWavToOpus(TestWavFile, config)
	if err != nil {
		t.Fatalf("转换WAV到Opus失败: %v", err)
	}

	logger.Infof("成功转换 %d 个Opus帧", len(opusFrames))

	// 验证转换结果
	if len(opusFrames) == 0 {
		t.Fatal("转换结果为空")
	}

	// 检查每个帧的大小
	for i, frame := range opusFrames {
		if len(frame) == 0 {
			t.Fatalf("第 %d 帧为空", i)
		}
		logger.Debugf("帧 %d: %d bytes", i, len(frame))
	}

	logger.Info("WAV转Opus转换测试完成")
}

// 测试UDP音频包构建
func TestUDPAudioPacket(t *testing.T) {
	logger.Info("开始UDP音频包构建测试...")

	// 生成测试nonce
	nonce := generateTestNonce()
	sequence := uint32(1)
	testAudioData := []byte{1, 2, 3, 4, 5} // 测试音频数据

	// 构建音频包
	packet := buildAudioPacketForTest(nonce, sequence, testAudioData)

	// 验证包大小
	expectedSize := 16 + 4 + 4 + len(testAudioData)
	if len(packet) != expectedSize {
		t.Fatalf("音频包大小错误，期望: %d，实际: %d", expectedSize, len(packet))
	}

	// 验证nonce
	for i := 0; i < 16; i++ {
		if packet[i] != nonce[i] {
			t.Fatalf("nonce验证失败，位置: %d", i)
		}
	}

	// 验证时间戳
	timestamp := binary.BigEndian.Uint32(packet[16:20])
	if timestamp == 0 {
		t.Fatal("时间戳为0")
	}

	// 验证序列号
	seq := binary.BigEndian.Uint32(packet[20:24])
	if seq != sequence {
		t.Fatalf("序列号错误，期望: %d，实际: %d", sequence, seq)
	}

	// 验证音频数据
	for i := 0; i < len(testAudioData); i++ {
		if packet[24+i] != testAudioData[i] {
			t.Fatalf("音频数据验证失败，位置: %d", i)
		}
	}

	logger.Info("UDP音频包构建测试完成")
}

// 测试完整UDP音频传输流程
func TestCompleteUDPAudioFlow(t *testing.T) {
	logger.Info("开始完整UDP音频传输流程测试...")

	// 测试配置
	const (
		UDPHost     = "localhost"
		UDPPort     = 8847
		TestWavFile = "./test_audio.wav"
	)

	// 检查测试音频文件
	if _, err := os.Stat(TestWavFile); os.IsNotExist(err) {
		logger.Warnf("测试音频文件不存在: %s，跳过完整流程测试", TestWavFile)
		t.Skip("测试音频文件不存在")
	}

	// 连接UDP服务器
	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", UDPHost, UDPPort))
	if err != nil {
		t.Fatalf("解析UDP地址失败: %v", err)
	}

	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		t.Fatalf("连接UDP服务器失败: %v", err)
	}
	defer conn.Close()

	// 转换WAV到Opus
	config := AudioConfig{
		SampleRate:    16000,
		Channels:      1,
		FrameDuration: 60,
	}

	opusFrames, err := ConvertWavToOpus(TestWavFile, config)
	if err != nil {
		t.Fatalf("转换WAV到Opus失败: %v", err)
	}

	logger.Infof("开始发送 %d 个音频帧", len(opusFrames))

	// 生成nonce
	nonce := generateTestNonce()
	sequence := uint32(1)
	sentCount := 0

	// 发送音频帧
	for i, frame := range opusFrames {
		// 构建音频包
		audioPacket := buildAudioPacketForTest(nonce, sequence, frame)

		// 发送音频包
		_, err := conn.Write(audioPacket)
		if err != nil {
			t.Fatalf("发送音频包失败: %v", err)
		}

		sentCount++
		logger.Debugf("发送音频包 %d/%d，大小: %d bytes", i+1, len(opusFrames), len(audioPacket))

		// 模拟实时发送间隔
		time.Sleep(60 * time.Millisecond) // 60ms帧间隔

		sequence++
	}

	logger.Infof("成功发送 %d 个音频包", sentCount)
	logger.Info("完整UDP音频传输流程测试完成")
}

// 生成测试nonce
func generateTestNonce() [16]byte {
	var nonce [16]byte
	// 模拟nonce: 01 + timestamp + sessionID + 0000000
	copy(nonce[0:2], []byte{0x01, 0x00})

	// 填充时间戳 (8字节)
	timestamp := uint64(time.Now().Unix())
	binary.BigEndian.PutUint64(nonce[2:10], timestamp)

	// 填充sessionID (6字节，简化处理)
	sessionID := "test123"
	copy(nonce[10:16], []byte(sessionID))

	return nonce
}

// ConvertWavToOpus 将WAV文件转换为Opus帧
// 这里应该使用你在udp_client_test.go中实现的代码
// func ConvertWavToOpus(wavPath string, config AudioConfig) ([][]byte, error) {
// 	// 注意：这里应该导入并使用你在udp_client_test.go中实现的ConvertWavToOpus函数
// 	// 由于包结构问题，这里提供一个简化的实现用于测试

// 	logger.Infof("转换WAV文件: %s", wavPath)
// 	logger.Infof("音频配置: SampleRate=%d, Channels=%d, FrameDuration=%d",
// 		config.SampleRate, config.Channels, config.FrameDuration)

// 	// 模拟生成一些Opus帧用于测试
// 	frames := make([][]byte, 0)
// 	for i := 0; i < 50; i++ { // 生成50个模拟帧
// 		frame := make([]byte, 100+i%50) // 模拟不同大小的帧
// 		for j := range frame {
// 			frame[j] = byte(i + j) // 填充一些数据
// 		}
// 		frames = append(frames, frame)
// 	}

// 	logger.Infof("生成了 %d 个Opus帧", len(frames))
// 	return frames, nil
// }

// 构建音频包
func buildAudioPacketForTest(nonce [16]byte, sequence uint32, audioData []byte) []byte {
	// 包格式: [16字节nonce][4字节时间戳][4字节序列号][音频数据]
	packet := make([]byte, 16+4+4+len(audioData))

	// 复制nonce
	copy(packet[0:16], nonce[:])

	// 时间戳
	timestamp := uint32(time.Now().Unix())
	binary.BigEndian.PutUint32(packet[16:20], timestamp)

	// 序列号
	binary.BigEndian.PutUint32(packet[20:24], sequence)

	// 音频数据
	copy(packet[24:], audioData)

	return packet
}

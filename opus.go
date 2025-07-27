package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-audio/wav"
	"github.com/hraban/opus"
	"github.com/uozi-tech/cosy/logger"
)

func OpusToWav(c *gin.Context, opusData [][]byte, chatMsgId string) (string, error) {
	dir := "statics/audio"
	// 确保目录存在
	if err := os.MkdirAll(dir, 0755); err != nil {
		logger.Error("创建目录失败:", err)
		return "", err
	}
	// Generate file name
	// tempFile, err := os.CreateTemp("", fmt.Sprintf("asr_%s_*.wav", chatMsgId))
	wavFile, err := os.Create(dir + fmt.Sprintf("/asr_%s_.wav", chatMsgId))
	if err != nil {
		return "", fmt.Errorf("failed to create Opus decoder: %v", err)
	}
	// Create Opus decoder (16kHz, mono)
	decoder, err := opus.NewDecoder(16000, 1)
	if err != nil {
		return "", fmt.Errorf("failed to create Opus decoder: %v", err)
	}

	var pcmData bytes.Buffer

	// Decode each Opus packet
	for _, opusPacket := range opusData {
		pcmFrame := make([]int16, 960) // 960 samples = 60ms at 16kHz
		n, err := decoder.Decode(opusPacket, pcmFrame)
		if err != nil {
			// Log error but continue with next packet
			logger.Errorf("Opus decode error: %v\n", err)
			continue
		}

		// Write decoded PCM data to buffer
		for i := 0; i < n; i++ {
			binary.Write(&pcmData, binary.LittleEndian, pcmFrame[i])
		}
	}

	// Create WAV file
	// defer tempFile.Close()
	defer wavFile.Close()
	// Write WAV header
	// writeWavHeader(tempFile, 1, 16000, 16, pcmData.Len())
	writeWavHeader(wavFile, 1, 16000, 16, pcmData.Len())
	// Write PCM data
	// if _, err := tempFile.Write(pcmData.Bytes()); err != nil {
	// 	return "", fmt.Errorf("failed to write PCM data: %v", err)
	// }
	if _, err := wavFile.Write(pcmData.Bytes()); err != nil {
		return "", fmt.Errorf("failed to write PCM data: %v", err)
	}
	return wavFile.Name(), nil
}

// Helper function to write WAV header
func writeWavHeader(file *os.File, numChannels int, sampleRate int, bitsPerSample int, dataSize int) {
	// RIFF header
	file.Write([]byte("RIFF"))
	binary.Write(file, binary.LittleEndian, uint32(36+dataSize)) // File size - 8
	file.Write([]byte("WAVE"))

	// Format chunk
	file.Write([]byte("fmt "))
	binary.Write(file, binary.LittleEndian, uint32(16))                                     // Chunk size
	binary.Write(file, binary.LittleEndian, uint16(1))                                      // Audio format (PCM)
	binary.Write(file, binary.LittleEndian, uint16(numChannels))                            // Num channels
	binary.Write(file, binary.LittleEndian, uint32(sampleRate))                             // Sample rate
	binary.Write(file, binary.LittleEndian, uint32(sampleRate*numChannels*bitsPerSample/8)) // Byte rate
	binary.Write(file, binary.LittleEndian, uint16(numChannels*bitsPerSample/8))            // Block align
	binary.Write(file, binary.LittleEndian, uint16(bitsPerSample))                          // Bits per sample

	// Data chunk
	file.Write([]byte("data"))
	binary.Write(file, binary.LittleEndian, uint32(dataSize)) // Data size
}

func GetAudioDuration(filePath string) (time.Duration, error) {
	// 打开 WAV 文件
	file, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	// 2. 创建解码器
	decoder := wav.NewDecoder(file)

	// 3. 获取持续时间
	duration, err := decoder.Duration()
	if err != nil {
		return 0, fmt.Errorf("计算持续时间失败: %w", err)
	}

	return duration, nil
}

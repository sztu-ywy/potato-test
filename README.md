# UDP + MQTT 集成测试

## 概述

本测试套件用于验证UDP音频传输和MQTT命令传输的完整流程，包括HTTP认证、MQTT握手、音频传输和会话管理。

## 测试文件说明

### 1. `integration_test.go`
完整的集成测试，包含以下测试流程：
- HTTP设备认证
- MQTT hello握手
- IoT消息传输
- listen状态管理
- UDP音频传输
- MQTT文本消息
- goodbye会话清理

### 2. `wav_to_opus_test.go`
音频处理相关测试：
- WAV到Opus转换测试
- UDP音频包构建测试
- 完整UDP音频传输流程测试

### 3. `run_test.sh`
测试运行脚本，自动检查依赖服务并运行所有测试。

## 测试配置

### 服务器配置
```bash
# HTTP服务器
ServerHost = "localhost"
ServerPort = 8080

# MQTT服务器
MQTTHost = "localhost"
MQTTPort = 1883

# UDP服务器
UDPHost = "localhost"
UDPPort = 8847
```

### 设备配置
```bash
DeviceMAC = "10:51:db:72:70:a8"
DeviceUUID = "3c05ed7d-eae5-41ec-aebf-c284c9ddce90"
```

### 音频配置
```bash
SampleRate = 16000
Channels = 1
FrameDuration = 60ms
```

## 运行测试

### 1. 准备环境

确保以下服务正在运行：
- 主应用服务器 (HTTP + UDP)
- MQTT Broker (如Mosquitto)
- gRPC ASR和Dialog服务

### 2. 准备测试音频文件

将测试用的WAV文件放在 `./test_audio.wav`

### 3. 运行测试

#### 运行所有测试
```bash
chmod +x test/run_test.sh
./test/run_test.sh
```

#### 运行单个测试
```bash
# 运行完整集成测试
go test -v -run TestIntegration ./test/

# 运行UDP音频传输测试
go test -v -run TestUDPAudioTransmission ./test/

# 运行WAV转Opus测试
go test -v -run TestWavToOpusConversion ./test/

# 运行音频包构建测试
go test -v -run TestUDPAudioPacket ./test/
```

## 测试流程详解

### 1. HTTP认证流程
```
设备 -> POST /devices/auth
      -> 发送设备信息 (MAC, UUID, 版本等)
      -> 服务端验证设备
      -> 返回MQTT配置信息
```

### 2. MQTT握手流程
```
设备 -> 连接MQTT Broker
      -> 订阅公共主题 device-server
      -> 发送hello消息
      -> 服务端验证MAC并生成sessionID
      -> 返回hello响应包含nonce
      -> 双方订阅MAC哈希化主题
```

### 3. UDP音频传输流程
```
设备 -> 转换WAV到Opus帧
      -> 为每个帧添加nonce头部
      -> 通过UDP发送音频包
      -> 服务端解析nonce获取sessionID
      -> 转发音频到gRPC ASR服务
      -> 接收TTS音频并返回
```

### 4. MQTT命令传输流程
```
设备 -> 在设备主题发送命令
      -> 服务端处理命令
      -> 在服务端主题返回响应
```

### 5. 会话清理流程
```
设备 -> 发送goodbye消息
      -> 服务端清理gRPC资源
      -> 取消订阅主题
      -> 清理UDP会话
```

## 音频包格式

### UDP音频包结构
```
[16字节nonce][4字节时间戳][4字节序列号][音频数据]
```

### Nonce格式
```
01 + timestamp(8字节) + sessionID(8字节) + 0000000
```

## 主题命名规则

### 公共主题
- 发布/订阅: `device-server`

### 设备专用主题
- 设备发布: `potato/{mac_hash}/td`
- 服务端发布: `potato/{mac_hash}/server`

## 错误处理

### 常见错误及解决方案

1. **HTTP认证失败**
   - 检查设备MAC是否在数据库中
   - 确认HTTP服务器正在运行

2. **MQTT连接失败**
   - 检查MQTT Broker是否运行
   - 确认连接参数正确

3. **UDP连接失败**
   - 检查UDP服务器是否运行
   - 确认端口配置正确

4. **音频转换失败**
   - 检查WAV文件格式
   - 确认音频配置参数

## 调试技巧

### 1. 启用详细日志
```bash
export LOG_LEVEL=debug
go test -v ./test/
```

### 2. 检查网络连接
```bash
# 检查HTTP服务
curl http://localhost:8080/health

# 检查MQTT服务
nc -z localhost 1883

# 检查UDP服务
nc -z localhost 8847
```

### 3. 监控MQTT消息
```bash
# 使用mosquitto_sub监控主题
mosquitto_sub -h localhost -t "device-server" -v
mosquitto_sub -h localhost -t "potato/+/td" -v
```

## 性能测试

### 音频传输性能
- 测试不同音频文件大小
- 测试不同帧率
- 测试并发设备数量

### MQTT消息性能
- 测试消息吞吐量
- 测试连接稳定性
- 测试重连机制

## 注意事项

1. **测试环境隔离**: 确保测试不影响生产环境
2. **资源清理**: 测试完成后及时清理资源
3. **数据验证**: 验证音频数据的完整性和正确性
4. **错误恢复**: 测试各种错误场景的恢复机制

## 扩展测试

### 1. 压力测试
- 模拟大量设备同时连接
- 测试系统负载能力

### 2. 故障测试
- 模拟网络中断
- 测试服务重启恢复

### 3. 安全测试
- 测试MAC地址验证
- 测试nonce安全性 # potato-test
